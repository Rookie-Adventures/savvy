package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/smartwalle/alipay/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ponytail: Gate3 (signature-fail rejection) 测试。
// 真实 SDK VerifySign 需支付宝公钥;公钥未加载/无效时 getVerifier 返 ErrAliPublicKeyNotFound
// (已 probe 确认 alipay.New+LoadAliPayPublicKey("dummy") → VerifySign 返 err)。
// 这正是 verify-fail 路径:签名校验失败 → handler 必写 "fail" 且不得落库/升级。
// 不重构生产代码建 seam;直接注入一个 VerifySign 必失败的真 *alipay.Client 到 alipayClient 单例。

// newAlipayClientWithUnverifiableSign 构造一个真 *alipay.Client,其 VerifySign 必返 err
// (公钥模式 + 无效公钥 → getVerifier 返 ErrAliPublicKeyNotFound)。
func newAlipayClientWithUnverifiableSign(t *testing.T) *alipay.Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privB64 := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))
	cli, err := alipay.New("2021000000000000", privB64, false)
	require.NoError(t, err)
	// ponytail: 故意不加载支付宝公钥 → VerifySign 走 getVerifier → ErrAliPublicKeyNotFound。
	// (LoadAliPayPublicKey 对无效输入会 err,但根本不调用更干净:client 无 verifier 即验签失败。)
	return cli
}

// Gate3 订阅链:验签失败 → 响应 "fail",订单仍 Pending,无 UserSubscription。
// 锁 subscription_payment_alipay.go:151-154 `if err := cli.VerifySign(...); err != nil { write "fail" }`。
func TestSubscriptionAlipayNotify_RejectsOnVerifySignFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	confirmPaymentComplianceForTest(t)
	// ponytail: 注入必失败的 alipay client(真 *alipay.Client,公钥无效 → VerifySign err)。
	cli := newAlipayClientWithUnverifiableSign(t)
	originalClient := alipayClient
	alipayClient = cli
	t.Cleanup(func() { alipayClient = originalClient })

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.User{}))
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: 1, Title: "pro", PriceAmount: 9.90, Enabled: true,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000,
	}).Error)
	// 预置一笔 pending 订阅订单(provider=alipay,模拟用户已下单)。
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 1, PlanId: 1, Money: 9.90, TradeNo: "SUB-VERIFYFAIL-1",
		PaymentMethod: model.PaymentMethodAlipay, PaymentProvider: model.PaymentProviderAlipay,
		Status: common.TopUpStatusPending, CreateTime: 1,
	}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "u", Status: common.UserStatusEnabled}).Error)

	// 构造一个 trade_status=TRADE_SUCCESS 但 sign 缺失/伪造的 alipay 异步通知表单。
	form := url.Values{}
	form.Set("out_trade_no", "SUB-VERIFYFAIL-1")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("total_amount", "9.90")
	// 故意不设 sign —— VerifySign 必返 err

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/alipay/notify", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	SubscriptionAlipayNotify(c)

	body := w.Body.String()
	assert.True(t, strings.Contains(body, "fail"),
		"verify-fail must write literal \"fail\", got: %s", body)

	// 订单必须仍 Pending —— 验签失败不得落库完成。
	order := model.GetSubscriptionOrderByTradeNo("SUB-VERIFYFAIL-1")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status,
		"verify-fail must NOT complete the order")
	var subCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 1).Count(&subCount).Error)
	assert.Zero(t, subCount, "verify-fail must NOT create UserSubscription")
}

// Gate3 topup 链:验签失败 → 响应 "fail",topup 仍 Pending,额度不变。
// 锁 topup_alipay.go:100-103 VerifySign err → write "fail" 早退(在 GetTopUpByTradeNo 之前)。
func TestAlipayNotify_RejectsOnVerifySignFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cli := newAlipayClientWithUnverifiableSign(t)
	originalClient := alipayClient
	alipayClient = cli
	t.Cleanup(func() { alipayClient = originalClient })

	// ponytail: IsAlipayConfigured 必须返 true 才能越过 handler 首个 gate。
	originalAppId := operation_setting.AlipayAppId
	originalPriv := operation_setting.AlipayAppPrivateKey
	originalPub := operation_setting.AlipayPublicKey
	t.Cleanup(func() {
		operation_setting.AlipayAppId = originalAppId
		operation_setting.AlipayAppPrivateKey = originalPriv
		operation_setting.AlipayPublicKey = originalPub
	})
	operation_setting.AlipayAppId = "2021000000000000"
	operation_setting.AlipayAppPrivateKey = "dummy"
	operation_setting.AlipayPublicKey = "dummy"

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.User{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "u", Status: common.UserStatusEnabled, Quota: 0}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 1, Amount: 10, Money: 9.90, TradeNo: "TOP-VERIFYFAIL-1",
		PaymentMethod: model.PaymentMethodAlipay, PaymentProvider: model.PaymentProviderAlipay,
		Status: common.TopUpStatusPending, CreateTime: 1,
	}).Error)

	form := url.Values{}
	form.Set("out_trade_no", "TOP-VERIFYFAIL-1")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("total_amount", "9.90")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/alipay/notify", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	AlipayNotify(c)

	body := w.Body.String()
	assert.True(t, strings.Contains(body, "fail"),
		"verify-fail must write literal \"fail\", got: %s", body)

	topUp := model.GetTopUpByTradeNo("TOP-VERIFYFAIL-1")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status, "verify-fail must NOT complete the topup")
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", 1).First(&user).Error)
	assert.Zero(t, user.Quota, "verify-fail must NOT increase user quota")
}

// ponytail: 旁证 — 显式断言注入的 client 的 VerifySign 确实返 err(锁测试前提成立)。
// 若未来 SDK 改为 empty-sign 静默通过,此测试会先 fail 暴露前提失效。
func TestInjectedAlipayClientVerifySignErrors(t *testing.T) {
	cli := newAlipayClientWithUnverifiableSign(t)
	v := url.Values{}
	v.Set("out_trade_no", "X")
	v.Set("trade_status", "TRADE_SUCCESS")
	err := cli.VerifySign(context.Background(), v)
	require.Error(t, err, "test fixture requires VerifySign to error; if this fails the Gate3 tests above are vacuous")
}

// ---- Gate 2 (idempotency) legit-completion + replay ----
// ponytail: model 包 TestMain 设 SetMaxOpenConns(1),CompleteSubscriptionOrder 正向完成路径
// (CreateUserSubscriptionFromPlanTx → GetDBTimestamp 开新连接)在单连接下死锁。
// controller 包 test DB 无此限制,故正向完成 + 重放幂等测试放这里。

// seedSubscriptionOrderForIdempotency 预置 user + plan + pending 订单,返回 tradeNo。
func seedSubscriptionOrderForIdempotency(t *testing.T, provider, method string) string {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "u", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: 1, Title: "pro", PriceAmount: 9.90, Enabled: true,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000,
	}).Error)
	tradeNo := "SUB-IDEMPOTENT-" + provider
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 1, PlanId: 1, Money: 9.90, TradeNo: tradeNo,
		PaymentMethod: method, PaymentProvider: provider,
		Status: common.TopUpStatusPending, CreateTime: 1,
	}).Error)
	return tradeNo
}

// 订阅正向完成(alipay + wechat 各一):同 provider 回调 → Status→Success + 创建一条 UserSubscription。
// 锁正向路径,防止 Gate1 测试因实现退化成"无条件拒绝"仍通过。
func TestCompleteSubscriptionOrder_LegitAlipayWechatProviderCompletes(t *testing.T) {
	for _, provider := range []struct{ provider, method string }{
		{model.PaymentProviderAlipay, model.PaymentMethodAlipay},
		{model.PaymentProviderWechat, model.PaymentMethodWechat},
	} {
		t.Run(provider.provider, func(t *testing.T) {
			tradeNo := seedSubscriptionOrderForIdempotency(t, provider.provider, provider.method)

			err := model.CompleteSubscriptionOrder(tradeNo, `{"ok":true}`, provider.provider, provider.method)
			require.NoError(t, err)

			after := model.GetSubscriptionOrderByTradeNo(tradeNo)
			require.NotNil(t, after)
			assert.Equal(t, common.TopUpStatusSuccess, after.Status)
			var subCount int64
			require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 1).Count(&subCount).Error)
			assert.Equal(t, int64(1), subCount, "exactly one UserSubscription on legit completion")
		})
	}
}

// 订阅重放幂等:同一合法 CompleteSubscriptionOrder 调 N 次 →
// UserSubscription 仅一条,ProviderPayload 保留第一次的(重放不覆盖)。
// 锁 model/subscription.go:574 `if Status==Success return nil` 早退(在 CreateUserSubscriptionFromPlanTx 之前)。
func TestCompleteSubscriptionOrder_IdempotentOnReplay(t *testing.T) {
	tradeNo := seedSubscriptionOrderForIdempotency(t, model.PaymentProviderAlipay, model.PaymentMethodAlipay)

	const replays = 5
	for i := 0; i < replays; i++ {
		// ponytail: 故意每次带不同 payload,证明重放不会覆盖第一次落库的 ProviderPayload。
		err := model.CompleteSubscriptionOrder(tradeNo, `{"replay":`+strconv.Itoa(i)+`}`, model.PaymentProviderAlipay, model.PaymentMethodAlipay)
		require.NoError(t, err, "replay #%d must return nil (idempotent), not error", i)
	}

	after := model.GetSubscriptionOrderByTradeNo(tradeNo)
	require.NotNil(t, after)
	assert.Equal(t, common.TopUpStatusSuccess, after.Status)
	var subCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 1).Count(&subCount).Error)
	assert.Equal(t, int64(1), subCount,
		"replay must NOT create duplicate UserSubscription — gate2 (Status==Success early-return) must fire")
	// ponytail: 第一次成功落库的 payload 应保留;后续重放因早退不应改写。
	assert.Contains(t, after.ProviderPayload, `"replay":0`,
		"ProviderPayload must be from the first completion, not overwritten by replays")
}
