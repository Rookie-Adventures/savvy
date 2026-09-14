package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// 未配置 → JSAPI 单例 nil → 友好拒绝(对齐 Native nil-guard)。
// ponytail: 必须用带 session 中间件的 engine 跑,ServeHTTP 才会注入 session,
// 否则 handler 里 sessions.Default(c) 直接 panic(纯 gin.CreateTestContext 无 session 中间件)。
func TestRequestWechatJsapiPayRejectsUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clearWechatConfig()
	t.Cleanup(func() { wechatJsapiSvc = nil; clearWechatConfig() })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/wechat/jsapi/pay",
		strings.NewReader(`{"amount":10}`))
	newWechatPayTestEngine().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "当前管理员未配置支付信息") {
		t.Fatalf("expected nil-client guard, got %s", w.Body.String())
	}
}

// session 无 openid(未走 OAuth)→ 返回 wechat_oauth_required,前端据此跳授权。
// ponytail: 用真实 RSA 私钥 PEM 让 core.NewClient 构造成功(svc 非 nil),从而越过 nil-guard
// 真正走到 openid 检查分支;不触网、不落库(在 openid 检查处即返回)。
func TestRequestWechatJsapiPayRejectsMissingOpenid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setWechatConfigured()
	t.Cleanup(func() { wechatJsapiSvc = nil; clearWechatConfig() })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/wechat/jsapi/pay",
		strings.NewReader(`{"amount":10}`))
	newWechatPayTestEngine().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "wechat_oauth_required") {
		t.Fatalf("expected wechat_oauth_required, got %s", w.Body.String())
	}
}

// ---- test helpers ----
func newWechatPayTestEngine() *gin.Engine {
	r := gin.New()
	store := cookie.NewStore([]byte("wechat-jsapi-test-secret"))
	r.Use(sessions.Sessions("wechat_jsapi_session", store))
	r.POST("/api/user/wechat/jsapi/pay", RequestWechatJsapiPay)
	return r
}

func clearWechatConfig() {
	operation_setting.WechatAppId = ""
	operation_setting.WechatMpAppId = ""
	operation_setting.WechatMchID = ""
	operation_setting.WechatMchSerial = ""
	operation_setting.WechatAPIv3Key = ""
	operation_setting.WechatPrivateKeyPEM = ""
	operation_setting.WechatPayPublicKeyId = ""
	operation_setting.WechatPayPublicKey = ""
}

// setWechatConfigured 用公钥模式(非平台证书自动下载)构造配置,使 core.NewClient 不触网即成功,
// 从而越过 nil-guard 真正走到 openid 检查分支(ponytail: 平台证书模式会在 NewClient 时同步下载证书,
// 对假商户号必败 → svc 变 nil,测不到 openid 分支;公钥模式无此网络依赖)。
func setWechatConfigured() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		panic(err)
	}
	operation_setting.WechatAppId = "wx-native-test"
	operation_setting.WechatMpAppId = "wx-mp-test"
	operation_setting.WechatMchID = "mch-test"
	operation_setting.WechatMchSerial = "serial-test"
	operation_setting.WechatAPIv3Key = "32byteapikey32byteapikey32byteapi"
	operation_setting.WechatPrivateKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	// 公钥模式:WechatPayPublicKey 给裸 base64(normalizeWechatPublicKey 会包成 BEGIN PUBLIC KEY),
	// 与 buildWechatCoreClient 的公钥分支一致 → 不下载平台证书。
	operation_setting.WechatPayPublicKeyId = "PUB_KEY_ID_test"
	operation_setting.WechatPayPublicKey = base64.StdEncoding.EncodeToString(pubDER)
}
