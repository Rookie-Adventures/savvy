package model

import (
	"time"
)

// SkillPayOrder 微信 Agent Pay X402 付费技能订单（一次 AI 问答 = 一单）。
// 幂等契约：Fulfilled 仅允许 false→true 原子翻转一次，重复重试返回缓存内容。
type SkillPayOrder struct {
	Id            int     `json:"id"`
	OutTradeNo    string  `json:"out_trade_no" gorm:"unique;type:varchar(32);index"` // WX402_+14+12=32 位
	PaymentCode   string  `json:"payment_code" gorm:"type:varchar(128);index"`       // X402 预下单返回
	Kind          string  `json:"kind" gorm:"type:varchar(20)"`                      // qa=付费问答 / topup=额度充值(服务包)
	MoneyYuan     float64 `json:"money_yuan"`                                        // topup: 用户申报充值金额（元）；qa: 单价
	Status        string  `json:"status" gorm:"type:varchar(20)"`                    // pending/paid/fulfilled/closed
	Fulfilled     bool    `json:"fulfilled"`
	TransactionId string  `json:"transaction_id" gorm:"type:varchar(64)"`
	PayerOpenid   string  `json:"payer_openid" gorm:"type:varchar(128);index"` // 付款人 Native-AppID 空间 openid(查单/回调解出),匹配 users.wechat_id 直入账
	// ClaimToken 充值单建单即预生成的认领凭据(crypto/rand hex32)。
	// 为什么必须在下单时生成：payment_code 是 15 分钟的一次性会话凭证，Agent 跨会话/换设备就找不回；
	// 旧实现只在履约那一刻才发凭据，于是"已付款 + 凭证丢失"= 钱躺在商户账户里无人能自助认领。
	// 预生成的链接在支付成功前没有任何价值（TopUp 行只在实付确认后创建），提前发不增加资损面。
	ClaimToken string `json:"claim_token" gorm:"type:varchar(64);index"`
	Content    string `json:"content" gorm:"type:text"` // 付费内容（AI 问答结果 / 认领凭据）
	// MpPushedAt 未履约回推去重标记（秒）。notify 会重投十几次，靠原子占位保证只推一条。
	MpPushedAt int64 `json:"mp_pushed_at"`
	CreateTime int64 `json:"create_time"`
}

const (
	SkillPayKindQA    = "qa"
	SkillPayKindTopUp = "topup"
)

const (
	SkillPayStatusPending   = "pending"
	SkillPayStatusPaid      = "paid"
	SkillPayStatusFulfilled = "fulfilled"
	SkillPayStatusClosed    = "closed"
)

func (o *SkillPayOrder) Insert() error {
	o.CreateTime = time.Now().Unix()
	return DB.Create(o).Error
}

func GetSkillPayOrderByTradeNo(tradeNo string) *SkillPayOrder {
	if tradeNo == "" {
		return nil
	}
	var order SkillPayOrder
	err := DB.Where("out_trade_no = ?", tradeNo).First(&order).Error
	if err != nil {
		return nil
	}
	return &order
}

// GetSkillPayOrderByClaimToken 按预生成的认领凭据反查技能单（用户只有链接、没有会话时的入口）。
func GetSkillPayOrderByClaimToken(claimToken string) *SkillPayOrder {
	if len(claimToken) != 32 {
		return nil
	}
	var order SkillPayOrder
	if err := DB.Where("claim_token = ?", claimToken).First(&order).Error; err != nil {
		return nil
	}
	return &order
}

// SetSkillPayOrderClaimToken 给历史单补记凭据（仅当原值为空，不覆盖已有钥匙）。
func SetSkillPayOrderClaimToken(tradeNo, claimToken string) error {
	if tradeNo == "" || claimToken == "" {
		return nil
	}
	return DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND claim_token = ?", tradeNo, "").
		Update("claim_token", claimToken).Error
}

// ClaimSkillPayMpPushOnce 回推去重占位：仅当 mp_pushed_at=0 时原子写入当前时间。
// 返回 true 表示本次调用抢到唯一推送资格（notify 重投/查单/兜底任务并发都只推一条）。
func ClaimSkillPayMpPushOnce(tradeNo string) bool {
	res := DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND mp_pushed_at = ?", tradeNo, 0).
		Update("mp_pushed_at", time.Now().Unix())
	return res.RowsAffected == 1 && res.Error == nil
}

// GetUnfulfilledSkillPayTopUpOrders 已支付(pending/paid 都可能)但未履约的充值单，供兜底任务扫。
// minAgeSec 避开刚下单的正常流程；createdAt 上限不设——历史死单也要能被捞起来。
func GetUnfulfilledSkillPayTopUpOrders(limit int, minAgeSec int64) ([]*SkillPayOrder, error) {
	if limit <= 0 {
		limit = 20
	}
	var orders []*SkillPayOrder
	err := DB.Where("kind = ? AND fulfilled = ? AND status IN ? AND create_time < ?",
		SkillPayKindTopUp, false,
		[]string{SkillPayStatusPending, SkillPayStatusPaid},
		time.Now().Unix()-minAgeSec).
		Order("create_time asc").Limit(limit).Find(&orders).Error
	return orders, err
}

// MarkSkillPayOrderPaid 回调/查单确认已支付：置 Status=paid 并记渠道交易号（幂等，已 paid/fulfilled 不降级）。
// payerOpenid 非空才写入（查单与回调先后到达时，空值不得覆盖已存的付款人身份）。
func MarkSkillPayOrderPaid(tradeNo, transactionId, payerOpenid string) error {
	updates := map[string]interface{}{"status": SkillPayStatusPaid, "transaction_id": transactionId}
	if payerOpenid != "" {
		updates["payer_openid"] = payerOpenid
	}
	return DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND status = ?", tradeNo, SkillPayStatusPending).
		Updates(updates).Error
}

// BindSkillPayPayerOpenid 补记付款人 openid（仅当原值为空；不覆盖已有身份）。
func BindSkillPayPayerOpenid(tradeNo, payerOpenid string) error {
	if tradeNo == "" || payerOpenid == "" {
		return nil
	}
	return DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND payer_openid = ?", tradeNo, "").
		Update("payer_openid", payerOpenid).Error
}

// MarkSkillPayOrderClosed 预下单失败关单。
func MarkSkillPayOrderClosed(tradeNo string) error {
	return DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND status = ?", tradeNo, SkillPayStatusPending).
		Update("status", SkillPayStatusClosed).Error
}

// MarkSkillPayOrderSweptClosed 兜底任务关单：超期仍未支付（含回调已标 paid 但查单非 SUCCESS 的异常单）。
// 已履约的单永不覆盖。
func MarkSkillPayOrderSweptClosed(tradeNo string) error {
	return DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND fulfilled = ? AND status IN ?", tradeNo, false,
			[]string{SkillPayStatusPending, SkillPayStatusPaid}).
		Update("status", SkillPayStatusClosed).Error
}

// FulfillSkillPayOrder 原子履约：仅当 fulfilled=false 时写入内容并翻转为 true。
// 返回 affected==1 表示本请求首次履约；==0 表示已被其他请求履约（调用方读缓存返回）。
func FulfillSkillPayOrder(tradeNo, transactionId, content string) (int64, error) {
	res := DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND fulfilled = ?", tradeNo, false).
		Updates(map[string]interface{}{
			"fulfilled":      true,
			"status":         SkillPayStatusFulfilled,
			"transaction_id": transactionId,
			"content":        content,
		})
	return res.RowsAffected, res.Error
}

// GetSkillPayOrderFulfilledContent 读取已履约内容（幂等重试缓存）。
func GetSkillPayOrderFulfilledContent(tradeNo string) (string, bool) {
	order := GetSkillPayOrderByTradeNo(tradeNo)
	if order == nil || !order.Fulfilled {
		return "", false
	}
	return order.Content, true
}
