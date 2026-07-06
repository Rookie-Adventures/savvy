package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func TestRequestAlipayPayRejectsUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	operation_setting.AlipayAppId = ""
	operation_setting.AlipayAppPrivateKey = ""
	operation_setting.AlipayPublicKey = ""
	operation_setting.AlipayIsCertMode = false

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/alipay/pay",
		strings.NewReader(`{"amount":10}`))

	RequestAlipayPay(c)
	if !strings.Contains(w.Body.String(), `"message":"error"`) &&
		!strings.Contains(w.Body.String(), `"message":"fail"`) {
		t.Fatalf("expected error when alipay unconfigured, got %s", w.Body.String())
	}
}
