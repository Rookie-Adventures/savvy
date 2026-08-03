package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/smartwalle/alipay/v3"
)

var alipayClient *alipay.Client

// GetAlipayClient returns a singleton alipay client built from operation_setting.
// Public-key mode uses AlipayAppId+AppPrivateKey+PublicKey; cert mode loads the 3 cert PEMs.
// nil if not configured (handlers gate on this and return friendly error).
func GetAlipayClient() *alipay.Client {
	if alipayClient != nil {
		return alipayClient
	}
	if !operation_setting.IsAlipayConfigured() {
		return nil
	}
	// ponytail: production(沙箱 vs 正式网关)与 cert-mode(验签方案)是两个正交开关。
	// 原实现硬编码 true → 沙箱也会指向正式网关且可能触发 SDK 在缺 verifier 时的自动下载证书回退。
	// 改读 AlipayIsProduction:admin 按需各设各的。
	c, err := alipay.New(operation_setting.AlipayAppId, operation_setting.AlipayAppPrivateKey, operation_setting.AlipayIsProduction)
	if err != nil {
		return nil
	}
	if operation_setting.AlipayIsCertMode {
		// ponytail: SDK v3.2.29 cert-mode loaders take PEM CONTENT (cert string), not SN.
		// Brief called LoadAppCertSN/LoadAliPayCertSN/LoadRootCertSN (SN-based) — those don't exist in v3.2.29.
		// Real methods: LoadAppCertPublicKey / LoadAlipayCertPublicKey / LoadAliPayRootCert.
		// Setting field names end in "...SN" (legacy plan naming) but actually hold PEM cert content here.
		if err = c.LoadAppCertPublicKey(operation_setting.AlipayAppCertSN); err != nil {
			return nil
		}
		if err = c.LoadAlipayCertPublicKey(operation_setting.AlipayAlipayCertSN); err != nil {
			return nil
		}
		if err = c.LoadAliPayRootCert(operation_setting.AlipayRootCertSN); err != nil {
			return nil
		}
	} else {
		if err = c.LoadAliPayPublicKey(operation_setting.AlipayPublicKey); err != nil {
			return nil
		}
	}
	alipayClient = c
	return alipayClient
}

// isMobileClient 按 UA 关键词判移动端,决定网站支付走 WAP 还是 PC 收银台。
// ponytail: 不引设备检测库,四个关键词覆盖主流移动 UA;iPadOS 默认桌面 UA 走 PC 分支无碍。
func isMobileClient(c *gin.Context) bool {
	ua := c.Request.UserAgent()
	return strings.Contains(ua, "Mobile") || strings.Contains(ua, "Android") ||
		strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad")
}

// alipayWebPayURL 网站支付统一下单:移动端用 TradeWapPay(QUICK_WAP_WAY,唤起支付宝 App),
// PC 保持 TradePagePay(FAST_INSTANT_TRADE_PAY)。两条链路共用同一回调与返跳。
func alipayWebPayURL(cli *alipay.Client, subject, tradeNo, totalAmount, notifyURL, returnURL string, wap bool) (*url.URL, error) {
	if wap {
		var p = alipay.TradeWapPay{}
		p.NotifyURL = notifyURL
		p.ReturnURL = returnURL
		p.Subject = subject
		p.OutTradeNo = tradeNo
		p.TotalAmount = totalAmount
		p.ProductCode = "QUICK_WAP_WAY"
		return cli.TradeWapPay(p)
	}
	var p = alipay.TradePagePay{}
	p.NotifyURL = notifyURL
	p.ReturnURL = returnURL
	p.Subject = subject
	p.OutTradeNo = tradeNo
	p.TotalAmount = totalAmount
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"
	return cli.TradePagePay(p)
}

type SubscriptionAlipayPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

// SubscriptionRequestAlipay creates a subscription order and returns an Alipay web-pay URL (WAP on mobile, PC page-pay otherwise).
func SubscriptionRequestAlipay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionAlipayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}
	// 注意:支付宝官方直连不读 PayMethods(那是易支付的支付方式枚举),只要走 alipay 一种。
	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}
	cli := GetAlipayClient()
	if cli == nil {
		common.ApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}
	tradeNo := fmt.Sprintf("SUBUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	callbackBase := service.GetCallbackAddress()
	notifyURL := callbackBase + "/api/subscription/alipay/notify"
	url, err := alipayWebPayURL(cli, plan.Title+"套餐", tradeNo,
		fmt.Sprintf("%.2f", plan.PriceAmount), notifyURL, paymentReturnPath("/console/topup"), isMobileClient(c))
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderAlipay)
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"pay_link": url.String()}})
}

// SubscriptionRequestAlipayQR creates a subscription order and returns an Alipay order-code (precreate) QR string.
// 与 SubscriptionRequestAlipay 同配置同回调,仅下单接口不同;回调复用 /api/subscription/alipay/notify。
func SubscriptionRequestAlipayQR(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionAlipayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}
	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}
	cli := GetAlipayClient()
	if cli == nil {
		common.ApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}
	tradeNo := fmt.Sprintf("SUBQRUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	callbackBase := service.GetCallbackAddress()
	var p = alipay.TradePreCreate{}
	p.NotifyURL = callbackBase + "/api/subscription/alipay/notify"
	// ponytail: subject 用"套餐名+套餐"(如"STARTER套餐"),去掉 SUB: 内部前缀,避免黑话漏进用户账单。
	p.Subject = plan.Title + "套餐"
	p.OutTradeNo = tradeNo
	p.TotalAmount = fmt.Sprintf("%.2f", plan.PriceAmount)
	// 订单码支付与当面付同产品码,二维码 2 小时有效(支付宝侧默认)。
	p.ProductCode = "FACE_TO_FACE_PAYMENT"
	rsp, err := cli.TradePreCreate(context.Background(), p)
	if err != nil || rsp == nil || rsp.Code != alipay.CodeSuccess || rsp.QRCode == "" {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderAlipay)
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"code_url": rsp.QRCode}})
}

// SubscriptionAlipayNotify handles Alipay async notify (must return literal "success").
func SubscriptionAlipayNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	cli := GetAlipayClient()
	if cli == nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	// ponytail: SDK v3.2.29 VerifySign(ctx, values) returns error ONLY (not (bool, error)).
	// Brief assumed (bool, error). Adapted: err != nil == verify failed.
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
	// 防重放/防重复:锁订单
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	payload := common.GetJsonString(c.Request.Form)
	// 防跨网关:expectedPaymentProvider=alipay 校验订单 provider 必须是 alipay
	if err := model.CompleteSubscriptionOrder(tradeNo, payload, model.PaymentProviderAlipay, model.PaymentMethodAlipay); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	// 关键:支付宝异步通知必须返回 "success" 否则支付宝重试 8 次
	_, _ = c.Writer.Write([]byte("success"))
}
