package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/smartwalle/alipay/v3"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

// 查单函数抽成包级变量:测试可 stub(对齐 wechatExchangeCodeFn 先例)。
var wechatQueryOrderFn = func(tradeNo string) (*payments.Transaction, error) {
	svc := GetWechatClient()
	if svc == nil {
		return nil, fmt.Errorf("微信支付未配置")
	}
	tx, _, err := svc.QueryOrderByOutTradeNo(context.Background(), native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(tradeNo),
		Mchid:      core.String(operation_setting.WechatMchID),
	})
	if err != nil {
		return nil, err
	}
	return tx, nil
}

var alipayQueryOrderFn = func(tradeNo string) (*model.TopUpAudit, error) {
	cli := GetAlipayClient()
	if cli == nil {
		return nil, fmt.Errorf("支付宝未配置")
	}
	var q = alipay.TradeQuery{}
	q.OutTradeNo = tradeNo
	rsp, err := cli.TradeQuery(context.Background(), q)
	if err != nil {
		return nil, err
	}
	if rsp == nil || rsp.Code != alipay.CodeSuccess {
		return nil, fmt.Errorf("支付宝查单返回异常")
	}
	if rsp.TradeStatus != "TRADE_SUCCESS" && rsp.TradeStatus != "TRADE_FINISHED" {
		return nil, fmt.Errorf("订单未支付成功(%s)", rsp.TradeStatus)
	}
	audit := alipayAuditFromQuery(rsp)
	return &audit, nil
}

// wechatAuditFromTx 从微信查单响应提取审计字段;非 SUCCESS 状态返回错误。
func wechatAuditFromTx(tx *payments.Transaction) (*model.TopUpAudit, error) {
	if tx == nil || tx.TradeState == nil || *tx.TradeState != "SUCCESS" {
		state := "UNKNOWN"
		if tx != nil && tx.TradeState != nil {
			state = *tx.TradeState
		}
		return nil, fmt.Errorf("订单未支付成功(%s)", state)
	}
	audit := model.TopUpAudit{}
	if tx.TransactionId != nil {
		audit.ChannelTradeNo = *tx.TransactionId
	}
	if tx.Payer != nil && tx.Payer.Openid != nil {
		audit.PayerId = *tx.Payer.Openid
	}
	if tx.SuccessTime != nil {
		if ts, err := time.Parse(time.RFC3339, *tx.SuccessTime); err == nil {
			audit.ChannelPayTime = ts.Unix()
		}
	}
	return &audit, nil
}

// BackfillTopUpChannelAudit 管理员触发:对成功但缺渠道交易号的支付宝/微信订单,
// 调官方查单 API 反查并回填审计字段。幂等,可重复触发。
// 历史订单的 balance_before/after 无法可靠重建,不回填(前端对 0 值自动隐藏余额行)。
func BackfillTopUpChannelAudit(c *gin.Context) {
	topups, err := model.GetTopUpsNeedingChannelBackfill(200)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "查询待回填订单失败: " + err.Error()})
		return
	}
	type failedItem struct {
		TradeNo string `json:"trade_no"`
		Reason  string `json:"reason"`
	}
	updated, failed := 0, []failedItem{}
	for _, tu := range topups {
		var audit *model.TopUpAudit
		var qerr error
		switch tu.PaymentProvider {
		case model.PaymentProviderWechat:
			var tx *payments.Transaction
			tx, qerr = wechatQueryOrderFn(tu.TradeNo)
			if qerr == nil {
				audit, qerr = wechatAuditFromTx(tx)
			}
		case model.PaymentProviderAlipay, model.PaymentProviderAlipayAgent:
			audit, qerr = alipayQueryOrderFn(tu.TradeNo)
		default:
			qerr = fmt.Errorf("不支持的渠道 %s", tu.PaymentProvider)
		}
		if qerr != nil {
			failed = append(failed, failedItem{TradeNo: tu.TradeNo, Reason: qerr.Error()})
			continue
		}
		username, email := "", ""
		if tu.UserId > 0 {
			username, _ = model.GetUsernameById(tu.UserId, false)
			email = model.GetUserEmailById(tu.UserId)
		}
		if uerr := model.UpdateTopUpChannelAudit(tu.TradeNo, *audit, username, email); uerr != nil {
			failed = append(failed, failedItem{TradeNo: tu.TradeNo, Reason: uerr.Error()})
			continue
		}
		updated++
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"scanned": len(topups),
		"updated": updated,
		"failed":  failed,
	}})
}
