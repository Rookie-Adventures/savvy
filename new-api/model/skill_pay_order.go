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
	Content       string  `json:"content" gorm:"type:text"` // 付费内容（AI 问答结果 / 认领凭据）
	CreateTime    int64   `json:"create_time"`
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

// MarkSkillPayOrderPaid 回调/查单确认已支付：置 Status=paid 并记渠道交易号（幂等，已 paid/fulfilled 不降级）。
func MarkSkillPayOrderPaid(tradeNo, transactionId string) error {
	return DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND status = ?", tradeNo, SkillPayStatusPending).
		Updates(map[string]interface{}{"status": SkillPayStatusPaid, "transaction_id": transactionId}).Error
}

// MarkSkillPayOrderClosed 预下单失败关单。
func MarkSkillPayOrderClosed(tradeNo string) error {
	return DB.Model(&SkillPayOrder{}).
		Where("out_trade_no = ? AND status = ?", tradeNo, SkillPayStatusPending).
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
