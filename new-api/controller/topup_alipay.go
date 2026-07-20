package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
)

// ponytail: brief's logger import `common/logger` was wrong — real path is `github.com/QuantumNous/new-api/logger` (see topup.go imports, EpayNotify uses logger.LogQuota).
// ponytail: brief's VerifySign(c.Request.Form) was wrong form — SDK v3.2.29 returns error only, takes (ctx, values). Mirrors Task 3 SubscriptionAlipayNotify.
// ponytail: TopUpStatusFailed exists in common/constants.go:262 (D chain added it). Used on TradePagePay failure so cleanup queue marks failed.

type AlipayTopUpRequest struct {
	Amount int64 `json:"amount"`
}

// RequestAlipayPay creates a wallet top-up order and returns an Alipay PC page-pay URL.
func RequestAlipayPay(c *gin.Context) {
	var req AlipayTopUpRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Amount <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	// ponytail: GetAlipayClient 早于 GetUserGroup — config 缺失直接 fail-fast,免 DB hit (epay 顺序反之但非契约)。
	cli := GetAlipayClient()
	if cli == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	userId := c.GetInt("id")
	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	tradeNo := fmt.Sprintf("ALIPAYUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	topUp := &model.TopUp{
		UserId:          userId,
		Amount:          req.Amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	callbackBase := service.GetCallbackAddress()
	var p = alipay.TradePagePay{}
	p.NotifyURL = callbackBase + "/api/user/alipay/notify"
	p.ReturnURL = paymentReturnPath("/console/log")
	p.Subject = "充值"
	p.OutTradeNo = tradeNo
	p.TotalAmount = strconv.FormatFloat(payMoney, 'f', 2, 64)
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"
	url, err := cli.TradePagePay(p)
	if err != nil {
		_ = model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderAlipay, common.TopUpStatusFailed)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"pay_link": url.String()}})
}

// AlipayNotify handles Alipay async notify for wallet top-up. Returns literal "success" on completion.
func AlipayNotify(c *gin.Context) {
	if !operation_setting.IsAlipayConfigured() {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	cli := GetAlipayClient()
	if cli == nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := cli.VerifySign(context.Background(), c.Request.Form); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if c.Request.Form.Get("trade_status") != "TRADE_SUCCESS" &&
		c.Request.Form.Get("trade_status") != "TRADE_FINISHED" {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	tradeNo := c.Request.Form.Get("out_trade_no")
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if topUp.PaymentProvider != model.PaymentProviderAlipay {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		// 幂等:已处理订单仍返 success 止 Alipay 8x 重试,不重复加钱
		_, _ = c.Writer.Write([]byte("success"))
		return
	}
	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = common.GetTimestamp()
	if err := topUp.Update(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	// 蚂蚁链存证: fire-and-forget, 状态已落库, 失败仅 SysError.
	if model.SubmitOrderEvidenceFn != nil {
		go func(in model.SubmitOrderEvidenceInput) {
			if err := model.SubmitOrderEvidenceFn(in); err != nil {
				common.SysError("antchain evidence submit failed: " + err.Error())
			}
		}(model.BuildTopupEvidence(topUp))
	}
	dAmount := decimal.NewFromInt(int64(topUp.Amount))
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
	if err := model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true); err != nil {
		// ponytail: latent money leak — Update 已落库 Success,IncreaseUserQuota 失败则钱到账未加额度。
		//   与 EpayNotify (topup.go:401-405) 同结构,parity 保留;model 层修复不在本任务范围。
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	model.RecordTopupLog(topUp.UserId,
		fmt.Sprintf("使用支付宝在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money),
		c.ClientIP(), topUp.PaymentMethod, model.PaymentMethodAlipay)
	_, _ = c.Writer.Write([]byte("success"))
}
