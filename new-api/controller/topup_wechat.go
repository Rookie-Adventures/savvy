package controller

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

type WechatTopUpRequest struct {
	Amount int64 `json:"amount"`
}

// RequestWechatPay creates a wallet top-up order and returns a wechat Native code_url.
func RequestWechatPay(c *gin.Context) {
	var req WechatTopUpRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Amount <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	// ponytail: GetWechatClient 早于 GetUserGroup — config 缺失直接 fail-fast,免 DB hit
	// (Task4 alipay 同模式已批准;epay 顺序反之但非契约)。
	// 无合规 gate(topup scope ≠ subscription)。
	svc := GetWechatClient()
	if svc == nil {
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
	tradeNo := fmt.Sprintf("WXUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	topUp := &model.TopUp{
		UserId:          userId,
		Amount:          req.Amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodWechat,
		PaymentProvider: model.PaymentProviderWechat,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	callbackBase := service.GetCallbackAddress()
	// ponytail: SDK v0.2.21 PrepayRequest 字段全 *string/*int64 指针;core.String/Int64 helper 取地址。
	resp, _, err := svc.Prepay(context.Background(), native.PrepayRequest{
		Appid:       core.String(operation_setting.WechatAppId),
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String("栗橙科技云服务费"),
		OutTradeNo:  core.String(tradeNo),
		NotifyUrl:   core.String(callbackBase + "/api/user/wechat/notify"),
		Amount: &native.Amount{
			// ponytail: IEEE-754 float→int64 截断丢部分分(19.90*100=1989 而非 1990),math.Round 恢复正确分。
			Total:    core.Int64(int64(math.Round(payMoney * 100))), // 微信金额单位=分
			Currency: core.String("CNY"),
		},
	})
	if err != nil {
		_ = model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderWechat, common.TopUpStatusFailed)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"code_url": *resp.CodeUrl}})
}

// WechatNotify handles wechat APIv3 async notify for wallet top-up.
// Returns {"code":"SUCCESS",...} on completion (wechat does not retry SUCCESS).
func WechatNotify(c *gin.Context) {
	if GetWechatClient() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "FAIL", "message": "unconfigured"})
		return
	}
	finalize := func(c *gin.Context, tradeNo, payload string) error {
		topUp := model.GetTopUpByTradeNo(tradeNo)
		if topUp == nil {
			return fmt.Errorf("order not found")
		}
		// 防跨网关:订单 provider 必须是 wechat
		if topUp.PaymentProvider != model.PaymentProviderWechat {
			return fmt.Errorf("provider mismatch")
		}
		if topUp.Status != common.TopUpStatusPending {
			// 幂等:已处理订单仍返 SUCCESS 止 wechat 重试,不重复加钱
			return nil
		}
		topUp.Status = common.TopUpStatusSuccess
		if err := topUp.Update(); err != nil {
			return err
		}
		dAmount := decimal.NewFromInt(int64(topUp.Amount))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if err := model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true); err != nil {
			// ponytail: latent money leak — Update 已落库 Success,IncreaseUserQuota 失败则钱到账未加额度。
			//   与 AlipayNotify (topup_alipay.go:134-138) + EpayNotify (topup.go:401-405) 同结构,parity 保留;model 层修复不在本任务范围。
			return err
		}
		model.RecordTopupLog(topUp.UserId,
			fmt.Sprintf("使用微信在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money),
			c.ClientIP(), topUp.PaymentMethod, model.PaymentMethodWechat)
		return nil
	}
	handleWxNotify(c, finalize)
}
