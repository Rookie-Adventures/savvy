package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// WeChatOAuthToken 一次性跨设备票证:桌面扫码登录/绑定与微信内直登共用。
// 桌面与手机不共享 session,故 state 采用「kind:token」自含票证 —— token 为
// crypto/rand 32 字节 hex(256bit),不可猜测即天然防 CSRF,无需服务端 session 关联。
//
// Status 状态机:
//
//	pending →(callback 成功)
//	  bind:            completed(rejected=openid 已被占用)
//	  login 已绑:      completed
//	  login 未绑:      authorized(openid_pending 落库)
//	  direct 已绑:     consumed(手机会话由 callback 直接 setupLogin 落地)
//	  direct 未绑:     authorized
//	authorized →(桌面 claim / 微信内 bind-existing)consumed
//
// ponytail: 过期行留库无危害(量小),不做后台清理;读时 IsExpired() 覆盖过期展示。
type WeChatOAuthToken struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	Token         string `json:"token" gorm:"type:varchar(128);uniqueIndex"`
	Kind          string `json:"kind" gorm:"type:varchar(16);index"` // bind | login | direct
	UserId        int    `json:"user_id"`                            // bind: 发起绑定的用户;login/direct 无归属(0)
	Status        string `json:"status" gorm:"type:varchar(16);index"`
	OpenidPending string `json:"openid_pending" gorm:"type:varchar(128)"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint"`
	ExpiresAt     int64  `json:"expires_at" gorm:"bigint;index"`
	CompletedAt   int64  `json:"completed_at" gorm:"bigint"`
}

const weChatOAuthTokenTTL = 5 * time.Minute

func CreateWeChatOAuthToken(kind string, userId int) (*WeChatOAuthToken, error) {
	switch kind {
	case "bind":
		if userId <= 0 {
			return nil, errors.New("bind token requires login user")
		}
	case "login", "direct":
	default:
		return nil, errors.New("invalid wechat oauth token kind")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	tok := &WeChatOAuthToken{
		Token:     hex.EncodeToString(buf),
		Kind:      kind,
		UserId:    userId,
		Status:    "pending",
		CreatedAt: now,
		ExpiresAt: now + int64(weChatOAuthTokenTTL.Seconds()),
	}
	if err := DB.Create(tok).Error; err != nil {
		return nil, err
	}
	return tok, nil
}

func GetWeChatOAuthTokenByToken(token string) (*WeChatOAuthToken, error) {
	if token == "" {
		return nil, errors.New("token is empty")
	}
	var tok WeChatOAuthToken
	err := DB.Where("token = ?", token).First(&tok).Error
	if err != nil {
		return nil, err
	}
	return &tok, nil
}

// ConsumeWeChatOAuthToken 条件更新:仅 pending 且未过期的票证可被消费一次,
// 单用性由 DB 的 RowsAffected 保证(并发下也只有一个请求成功)。
// userId>0 时落 user_id(login 消费落绑定用户);userId=0 保留原值(bind 票证归属不可被清掉)。
func ConsumeWeChatOAuthToken(token, newStatus string, userId int, openidPending string) error {
	updates := map[string]interface{}{
		"status":         newStatus,
		"openid_pending": openidPending,
		"completed_at":   time.Now().Unix(),
	}
	if userId > 0 {
		updates["user_id"] = userId
	}
	res := DB.Model(&WeChatOAuthToken{}).
		Where("token = ? AND status = ? AND expires_at > ?", token, "pending", time.Now().Unix()).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("wechat oauth token unavailable")
	}
	return nil
}

func (t *WeChatOAuthToken) IsExpired() bool {
	return t.ExpiresAt > 0 && t.ExpiresAt <= time.Now().Unix()
}
