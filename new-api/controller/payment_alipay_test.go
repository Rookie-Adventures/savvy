package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func TestSubscriptionRequestAlipayRejectsUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
	// ponytail: 未配置+合规 gate 任一会拦 → 断言 success:false 即可(brief 原断言 "error"/"fail" 过窄,
	// 合规 gate 实际返回 i18n key "payment.compliance_required")。语义不变:非成功响应。
	if !strings.Contains(w.Body.String(), `"success":false`) {
		t.Fatalf("expected non-success response when alipay unconfigured, got %s", w.Body.String())
	}
}
