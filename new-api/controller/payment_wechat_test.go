package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionRequestWechatRejectsUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// ponytail: 解锁合规(同时设 ComplianceConfirmed+ComplianceTermsVersion 并 defer 复原),
	// 让执行越过 requirePaymentCompliance 到达 nil-client guard,而非停在合规 gate。
	confirmPaymentComplianceForTest(t)
	// ponytail: 复位单例(GetWechatClient 短路 `if wechatNativeSvc != nil`),防其他测试初始化后本测试静默越过 nil-guard。
	t.Cleanup(func() { wechatNativeSvc = nil })
	// handler 在 nil-guard 前会 GetSubscriptionPlanById → 需要真实 DB + 一行启用套餐才能走到 guard。
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: 1, Title: "pro", PriceAmount: 9.90, Enabled: true,
	}).Error)
	operation_setting.WechatAppId = ""

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/wechat/pay",
		strings.NewReader(`{"plan_id":1}`))

	SubscriptionRequestWechat(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	// ponytail: 断言精确命中 nil-client guard 写的 message,证明真正走到 handler 而非被合规 gate 拦下。
	if !strings.Contains(w.Body.String(), "当前管理员未配置支付信息") {
		t.Fatalf("expected nil-client guard message, got %s", w.Body.String())
	}
}

func TestRequestWechatPayRejectsUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	operation_setting.WechatAppId = ""
	operation_setting.WechatMchID = ""
	operation_setting.WechatMchSerial = ""
	operation_setting.WechatAPIv3Key = ""
	operation_setting.WechatPrivateKeyPEM = ""
	// ponytail: GetWechatClient 短路包级单例 wechatNativeSvc,若前测初始化过则本测清空 config 也无效 → 返非 nil → handler 越过 nil-guard 到 GetUserGroup 无 DB fixture → panic。t.Cleanup 复位保顺序无关+fork-safe。
	t.Cleanup(func() { wechatNativeSvc = nil })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/wechat/pay",
		strings.NewReader(`{"amount":10}`))

	RequestWechatPay(c)
	// ponytail: 断言精确命中 nil-client guard 写的 message,证明真走到 config-missing gate(早于 GetUserGroup,免 DB hit)。
	if !strings.Contains(w.Body.String(), "当前管理员未配置支付信息") {
		t.Fatalf("expected nil-client guard message, got %s", w.Body.String())
	}
}

// TestRequestWechatPayRejectsAppIdMissing: 用户当前真实状态 — 商户号等 5 项已配齐但缺已认证服务号 AppId。
// IsWechatConfigured() 必须返 false → GetWechatClient() 返 nil → handler 命中 nil-guard 友好拒绝,无 SDK panic。
// 锁 APPID 单字段缺失即拒,不卡支付宝(独立 provider)。拿到 AppId 后此路径自然转通。
func TestRequestWechatPayRejectsAppIdMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 5 项齐,仅 APPID 空(用户当下态)
	operation_setting.WechatAppId = ""
	operation_setting.WechatMchID = "1900000001"
	operation_setting.WechatMchSerial = "serial-abc"
	operation_setting.WechatAPIv3Key = "32byteapikey32byteapikey32byteapi"
	operation_setting.WechatPrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----"
	// ponytail: 复位单例 — 前测若已初始化非空 svc,本测改 config 不生效(短路返旧 svc → 越过 nil-guard)。
	t.Cleanup(func() { wechatNativeSvc = nil })

	// 先断言 setting 层 IsWechatConfigured()=false(APPID 缺即拒)
	if operation_setting.IsWechatConfigured() {
		t.Fatal("IsWechatConfigured should be false when WechatAppId empty even if other 5 fields filled")
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/wechat/pay",
		strings.NewReader(`{"amount":10}`))

	RequestWechatPay(c)
	// handler 必须命中 nil-client guard 友好拒绝,不得 panic/crash 或调 SDK
	if !strings.Contains(w.Body.String(), "当前管理员未配置支付信息") {
		t.Fatalf("expected nil-client guard message when APPID missing, got %s", w.Body.String())
	}
}
