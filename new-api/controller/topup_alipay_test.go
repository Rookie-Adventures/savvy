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
	// ponytail: GetAlipayClient 短路包级单例 alipayClient,若前测初始化过则本测清空 config 也无效 → 返非 nil → handler 越过 nil-guard 到 GetUserGroup 无 DB fixture → panic。t.Cleanup 复位保顺序无关+fork-safe。
	t.Cleanup(func() { alipayClient = nil })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/alipay/pay",
		strings.NewReader(`{"amount":10}`))

	RequestAlipayPay(c)
	// ponytail: 断言精确命中 nil-client guard 写的 message,证明真走到 config-missing gate,而非被 ShouldBindJSON 错误路径(同样写 "message":"error")掩盖。
	if !strings.Contains(w.Body.String(), "当前管理员未配置支付信息") {
		t.Fatalf("expected nil-client guard message, got %s", w.Body.String())
	}
}
