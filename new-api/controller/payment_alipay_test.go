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

func TestSubscriptionRequestAlipayRejectsUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// ponytail: 解锁合规(同时设 ComplianceConfirmed+ComplianceTermsVersion 并 defer 复原),
	// 让执行越过 requirePaymentCompliance 到达 nil-client guard(Step 4),而非停在合规 gate。
	confirmPaymentComplianceForTest(t)
	// ponytail: 复位单例(GetAlipayClient 短路 `if alipayClient != nil`),防其他测试初始化后本测试静默越过 nil-guard。
	t.Cleanup(func() { alipayClient = nil })
	// handler 在 nil-guard 前会 GetSubscriptionPlanById → 需要真实 DB + 一行启用套餐才能走到 guard。
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: 1, Title: "pro", PriceAmount: 9.90, Enabled: true,
	}).Error)
	operation_setting.AlipayAppId = ""
	operation_setting.AlipayAppPrivateKey = ""
	operation_setting.AlipayPublicKey = ""
	operation_setting.AlipayIsCertMode = false

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/alipay/pay",
		strings.NewReader(`{"plan_id":1,"payment_method":"alipay"}`))

	SubscriptionRequestAlipay(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	// ponytail: 断言精确命中 nil-client guard 写的 message,证明真正走到 Step 4 而非被合规 gate 拦下。
	if !strings.Contains(w.Body.String(), "当前管理员未配置支付信息") {
		t.Fatalf("expected nil-client guard message, got %s", w.Body.String())
	}
}
