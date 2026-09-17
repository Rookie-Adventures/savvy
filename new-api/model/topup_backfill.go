package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// UpdateTopUpChannelAudit 为已完成但缺渠道证据的订单回填审计字段(渠道查单补录)。
// 只填空:channel_trade_no/payer_id/payer_account/channel_pay_time/credited_username/credited_email
// 均带 WHERE channel_trade_no = '' 守卫,绝不覆盖已有数据(并发/重复触发安全)。
// balance_before/after 历史订单无法可靠重建,不回填(前端对 0 值自动隐藏余额行)。
func UpdateTopUpChannelAudit(tradeNo string, audit TopUpAudit, creditedUsername string, creditedEmail string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	updates := map[string]interface{}{}
	if audit.ChannelTradeNo != "" {
		updates["channel_trade_no"] = audit.ChannelTradeNo
		updates["payer_id"] = audit.PayerId
		updates["payer_account"] = audit.PayerAccount
		if audit.ChannelPayTime > 0 {
			updates["channel_pay_time"] = audit.ChannelPayTime
		}
	}
	if creditedUsername != "" {
		updates["credited_username"] = creditedUsername
	}
	if creditedEmail != "" {
		updates["credited_email"] = creditedEmail
	}
	if len(updates) == 0 {
		return nil
	}
	result := DB.Model(&TopUp{}).
		Where(refCol+" = ? AND (channel_trade_no = '' OR channel_trade_no IS NULL)", tradeNo).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 已有渠道号(重复触发)或订单不存在:查一次区分,避免误报
		var count int64
		DB.Model(&TopUp{}).Where(refCol+" = ?", tradeNo).Count(&count)
		if count == 0 {
			return ErrTopUpNotFound
		}
	}
	// 老版本完成订单时没写 complete_time:有渠道支付时间的用渠道时间补齐
	DB.Model(&TopUp{}).
		Where(refCol+" = ? AND complete_time = 0 AND channel_pay_time > 0", tradeNo).
		Update("complete_time", gorm.Expr("channel_pay_time"))
	return nil
}

// GetUserEmailById 返回用户邮箱(不存在或为空返回空串)。
func GetUserEmailById(id int) string {
	var user User
	err := DB.Select("email").Where("id = ?", id).First(&user).Error
	if err != nil {
		return ""
	}
	return user.Email
}

// GetTopUpsNeedingChannelBackfill 列出已完成但缺渠道交易号的支付宝/微信订单(回填扫描)。
func GetTopUpsNeedingChannelBackfill(limit int) ([]*TopUp, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var topups []*TopUp
	err := DB.Where("status = ? AND (channel_trade_no = '' OR channel_trade_no IS NULL) AND payment_provider IN ?",
		common.TopUpStatusSuccess,
		[]string{PaymentProviderAlipay, PaymentProviderAlipayAgent, PaymentProviderWechat},
	).Order("id desc").Limit(limit).Find(&topups).Error
	return topups, err
}
