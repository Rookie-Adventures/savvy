package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

var wechatNativeSvc *native.NativeApiService

// GetWechatClient returns the wechat native-pay service; nil if not configured.
// AppId 缺时返 nil → handler 友好拒绝(用户当前态:商户号已有缺 AppId)。
func GetWechatClient() *native.NativeApiService {
	if wechatNativeSvc != nil {
		return wechatNativeSvc
	}
	if !operation_setting.IsWechatConfigured() {
		return nil
	}
	// ponytail: brief 假设 wxpay.NewConfig/NewClient(form) 不存在 on v0.2.21。
	// 真 SDK 形态: utils.LoadPrivateKey 解析 PEM → core.NewClient(ctx, option.WithWechatPayAutoAuthCipher(...))
	// 一键装配 signer+verifier+自动下载平台证书(覆盖 WechatPlatformCertPath,故 IsWechatConfigured 不校验它)。
	privKey, err := utils.LoadPrivateKey(operation_setting.WechatPrivateKeyPEM)
	if err != nil {
		return nil
	}
	// ponytail: WithWechatPayAutoAuthCipher 内部走 downloader.MgrInstance().RegisterDownloaderWithPrivateKey
	// 首次调用同步下载平台证书(网络),HasDownloader 二次幂等。verifier 源 = 同 mgr.GetCertificateVisitor(mchID)。
	client, err := core.NewClient(
		context.Background(),
		option.WithWechatPayAutoAuthCipher(
			operation_setting.WechatMchID,
			operation_setting.WechatMchSerial,
			privKey,
			operation_setting.WechatAPIv3Key,
		),
	)
	if err != nil {
		return nil
	}
	// ponytail: SDK v0.2.21 无 NewNativeApiService 构造器(docs/payments/native/NativeApi.md:60 用法)
	// NativeApiService 是 type NativeApiService services.Service{Client *core.Client},直接结构体字面量初始化。
	wechatNativeSvc = &native.NativeApiService{Client: client}
	return wechatNativeSvc
}

type SubscriptionWechatPayRequest struct {
	PlanId int `json:"plan_id"`
}

// SubscriptionRequestWechat: 微信 Native(扫码)下单,返回 code_url 前端生成二维码。
func SubscriptionRequestWechat(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionWechatPayRequest
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
	svc := GetWechatClient()
	if svc == nil {
		common.ApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}
	tradeNo := fmt.Sprintf("SUBWXUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodWechat,
		PaymentProvider: model.PaymentProviderWechat,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	callbackBase := service.GetCallbackAddress()
	// ponytail: SDK v0.2.21 PrepayRequest 字段全为 *string/*int64 指针(非裸 string/int64),
	// MarshalJSON 强制校验必填字段非 nil。core.String/Int64 helper 取地址(等价 &val,更短)。
	resp, _, err := svc.Prepay(context.Background(), native.PrepayRequest{
		Appid:       core.String(operation_setting.WechatAppId),
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String(fmt.Sprintf("SUB:%s", plan.Title)),
		OutTradeNo:  core.String(tradeNo),
		NotifyUrl:   core.String(callbackBase + "/api/subscription/wechat/notify"),
		Amount: &native.Amount{
			Total:    core.Int64(int64(plan.PriceAmount * 100)), // 微信金额单位=分
			Currency: core.String("CNY"),
		},
	})
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderWechat)
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"code_url": *resp.CodeUrl}})
}

// SubscriptionWechatNotify: 微信 APIv3 异步通知(JSON+签名头)。SDK 解密+验签后调 CompleteSubscriptionOrder。
func SubscriptionWechatNotify(c *gin.Context) {
	if GetWechatClient() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "FAIL", "message": "unconfigured"})
		return
	}
	finalize := func(c *gin.Context, tradeNo, payload string) error {
		// 防跨网关:expectedPaymentProvider=wechat 校验订单 provider 必须是 wechat
		if err := model.CompleteSubscriptionOrder(tradeNo, payload, model.PaymentProviderWechat, model.PaymentMethodWechat); err != nil {
			return err
		}
		return nil
	}
	handleWxNotify(c, finalize)
}
