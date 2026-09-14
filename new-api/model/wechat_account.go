package model

import (
	"errors"

	"gorm.io/gorm"
)

// WeChatAccount 微信身份绑定:微信只是第三方 identity provider,users/user_id 永远是核心。
// (provider, app_id, openid) 唯一 —— 不同 AppID(服务号/小程序/开放平台)的 openid 天然隔离;
// (user_id, provider, app_id) 唯一 —— 一个用户在同一 provider+app 下只绑一个微信。
// 老 users.wechat_id 路径(controller/wechat.go)保持原样,本表与之并存互不影响。
type WeChatAccount struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex:ux_wx_user_prov_app,priority:1"`
	Provider  string `json:"provider" gorm:"type:varchar(32);uniqueIndex:ux_wx_prov_app_open,priority:1;uniqueIndex:ux_wx_user_prov_app,priority:2"`
	AppId     string `json:"app_id" gorm:"type:varchar(128);uniqueIndex:ux_wx_prov_app_open,priority:2;uniqueIndex:ux_wx_user_prov_app,priority:3"`
	Openid    string `json:"openid" gorm:"type:varchar(128);uniqueIndex:ux_wx_prov_app_open,priority:3"`
	Unionid   string `json:"unionid" gorm:"type:varchar(128)"` // ponytail: 暂空,开放平台绑定后回填
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

var ErrWeChatAccountTaken = errors.New("该微信已绑定其他账户")

func GetWeChatAccountByOpenid(provider, appId, openid string) (*WeChatAccount, error) {
	if openid == "" {
		return nil, errors.New("openid is empty")
	}
	var acc WeChatAccount
	err := DB.Where("provider = ? AND app_id = ? AND openid = ?", provider, appId, openid).First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

func GetWeChatAccountByUserId(userId int, provider, appId string) (*WeChatAccount, error) {
	var acc WeChatAccount
	err := DB.Where("user_id = ? AND provider = ? AND app_id = ?", userId, provider, appId).First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// CreateWeChatAccount 先查占用再插入,占用则 err —— 绝不覆盖既有绑定(规格红线);
// 两把唯一索引作为并发兜底。
func CreateWeChatAccount(acc *WeChatAccount) error {
	if acc.UserId <= 0 || acc.Provider == "" || acc.AppId == "" || acc.Openid == "" {
		return errors.New("wechat account fields incomplete")
	}
	_, err := GetWeChatAccountByOpenid(acc.Provider, acc.AppId, acc.Openid)
	if err == nil {
		return ErrWeChatAccountTaken
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	_, err = GetWeChatAccountByUserId(acc.UserId, acc.Provider, acc.AppId)
	if err == nil {
		return errors.New("用户已绑定微信账户")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return DB.Create(acc).Error
}

func DeleteWeChatAccountById(id int) error {
	return DB.Delete(&WeChatAccount{}, id).Error
}
