package controller

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

var (
	wechatNativeSvc *native.NativeApiService
	wechatJsapiSvc  *jsapi.JsapiApiService
)

// normalizeWechatPublicKey: 商户平台「微信支付公钥」下发的是单行 base64(无 PEM 头尾),
// 而 utils.LoadPublicKey 只认 PEM 块 → 缺头尾时补上并按 64 字符折行。
func normalizeWechatPublicKey(s string) string {
	if strings.Contains(s, "BEGIN") {
		return s
	}
	clean := strings.Join(strings.Fields(s), "")
	var sb strings.Builder
	sb.WriteString("-----BEGIN PUBLIC KEY-----\n")
	for i := 0; i < len(clean); i += 64 {
		sb.WriteString(clean[i:min(i+64, len(clean))])
		sb.WriteString("\n")
	}
	sb.WriteString("-----END PUBLIC KEY-----\n")
	return sb.String()
}

// wechatUsePublicKeyVerifier 是"请求/回调同模式"铁律的唯一决策点(2026-09-12 事故):
// 公钥模式=true 走公钥 verifier;否则=false 走平台证书下载器。buildWechatCoreClient 与
// getWechatVerifier 都必须调它,绝不能在两处各写一遍条件(那正是事故根因)。
func wechatUsePublicKeyVerifier() bool {
	return operation_setting.WechatPayPublicKeyId != "" && operation_setting.WechatPayPublicKey != ""
}

// buildWechatCoreClient 是唯一装配 core.Client 的地方:公钥模式与平台证书模式二选一,
// Native/JSAPI 共用 → 保证两端同模式(2026-09-12 事故铁律)。
// ponytail: 公钥模式下 WithWechatPayPublicKeyAuthCipher 不依赖平台证书下载器,
// 老商户号才走 WithWechatPayAutoAuthCipher(内部下载平台证书)。
func buildWechatCoreClient() (*core.Client, error) {
	if !operation_setting.IsWechatConfigured() {
		return nil, fmt.Errorf("wechat pay not configured")
	}
	privKey, err := utils.LoadPrivateKey(operation_setting.WechatPrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("load merchant private key: %w", err)
	}
	if wechatUsePublicKeyVerifier() {
		pub, pubErr := utils.LoadPublicKey(normalizeWechatPublicKey(operation_setting.WechatPayPublicKey))
		if pubErr != nil {
			return nil, fmt.Errorf("load wechat public key: %w", pubErr)
		}
		return core.NewClient(
			context.Background(),
			option.WithWechatPayPublicKeyAuthCipher(
				operation_setting.WechatMchID,
				operation_setting.WechatMchSerial,
				privKey,
				operation_setting.WechatPayPublicKeyId,
				pub,
			),
		)
	}
	return core.NewClient(
		context.Background(),
		option.WithWechatPayAutoAuthCipher(
			operation_setting.WechatMchID,
			operation_setting.WechatMchSerial,
			privKey,
			operation_setting.WechatAPIv3Key,
		),
	)
}

// GetWechatClient 返回 Native 支付单例;nil 表示未配置(调用方友好拒绝)。
func GetWechatClient() *native.NativeApiService {
	if wechatNativeSvc != nil {
		return wechatNativeSvc
	}
	client, err := buildWechatCoreClient()
	if err != nil {
		logger.LogError(context.Background(), fmt.Sprintf("wechat pay: build native client failed: %v", err))
		return nil
	}
	wechatNativeSvc = &native.NativeApiService{Client: client}
	return wechatNativeSvc
}

// GetWechatJsapiClient 返回 JSAPI 支付单例;复用 buildWechatCoreClient → 与 Native/notify 同模式。
// ponytail: 包级单例(非 sync.Once),以便测试在 cleanup 中复位 wechatJsapiSvc=nil 强制重算(nil-guard 分支)。
func GetWechatJsapiClient() *jsapi.JsapiApiService {
	if wechatJsapiSvc != nil {
		return wechatJsapiSvc
	}
	client, err := buildWechatCoreClient()
	if err != nil {
		logger.LogError(context.Background(), fmt.Sprintf("wechat pay: build jsapi client failed: %v", err))
		return nil
	}
	wechatJsapiSvc = &jsapi.JsapiApiService{Client: client}
	return wechatJsapiSvc
}

// getWechatVerifier 是唯一装配 APIv3 回调验签器的地方,与 buildWechatCoreClient 同模式选择。
// ponytail: 事故铁律——notify 必须复用此函数,绝不在 wechat_notify.go 再复制平台证书路径。
func getWechatVerifier() (auth.Verifier, error) {
	if wechatUsePublicKeyVerifier() {
		pub, pubErr := utils.LoadPublicKey(normalizeWechatPublicKey(operation_setting.WechatPayPublicKey))
		if pubErr != nil {
			return nil, fmt.Errorf("load wechat public key: %w", pubErr)
		}
		return verifiers.NewSHA256WithRSAPubkeyVerifier(operation_setting.WechatPayPublicKeyId, *pub), nil
	}
	return verifiers.NewSHA256WithRSAVerifier(
		downloader.MgrInstance().GetCertificateVisitor(operation_setting.WechatMchID),
	), nil
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
		Description: core.String("栗橙科技-" + plan.Title + "套餐"),
		OutTradeNo:  core.String(tradeNo),
		NotifyUrl:   core.String(callbackBase + "/api/subscription/wechat/notify"),
		Amount: &native.Amount{
			// ponytail: IEEE-754 float→int64 截断丢部分分(19.90*100=1989 而非 1990),math.Round 恢复正确分。
			Total:    core.Int64(int64(math.Round(plan.PriceAmount * 100))), // 微信金额单位=分
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

// SubscriptionRequestWechatJsapi: 订阅 JSAPI 下单,返回调起参数;回调复用 /api/subscription/wechat/notify。
// 与 SubscriptionRequestWechat 唯一区别:走 JSAPI 单例 + Payer.Openid + 返调起参数(非 code_url)。
// 字段名以 go doc v0.2.21 为准:请求类型 jsapi.PrepayRequest,响应 Appid/TimeStamp/NonceStr/Package/SignType/PaySign。
func SubscriptionRequestWechatJsapi(c *gin.Context) {
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
	session := sessions.Default(c)
	openid, _ := session.Get(wechatJsapiOpenidSessionKey).(string)
	if openid == "" {
		c.JSON(http.StatusOK, gin.H{"message": "wechat_oauth_required", "data": nil})
		return
	}
	svc := GetWechatJsapiClient()
	if svc == nil {
		common.ApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}
	userId := c.GetInt("id")
	tradeNo := fmt.Sprintf("WXJSUB%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
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
	resp, _, err := svc.PrepayWithRequestPayment(context.Background(), jsapi.PrepayRequest{
		Appid:       core.String(operation_setting.WechatMpAppId), // 同充值:服务号 AppID
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String("栗橙科技-" + plan.Title + "套餐"),
		OutTradeNo:  core.String(tradeNo),
		NotifyUrl:   core.String(callbackBase + "/api/subscription/wechat/notify"),
		Amount: &jsapi.Amount{
			Total:    core.Int64(int64(math.Round(plan.PriceAmount * 100))),
			Currency: core.String("CNY"),
		},
		Payer: &jsapi.Payer{Openid: core.String(openid)},
	})
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderWechat)
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"appId":     *resp.Appid,
		"timeStamp": *resp.TimeStamp,
		"nonceStr":  *resp.NonceStr,
		"package":   *resp.Package,
		"signType":  *resp.SignType,
		"paySign":   *resp.PaySign,
	}})
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
