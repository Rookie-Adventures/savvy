package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newJsapiOauthTestEngine() *gin.Engine {
	r := gin.New()
	r.Use(sessions.Sessions("wechat_jsapi_session", cookie.NewStore([]byte("oauth-test-secret"))))
	r.GET("/api/user/wechat/jsapi/oauth/start", WechatJsapiOauthStart)
	r.GET("/api/user/wechat/jsapi/oauth/callback", WechatJsapiOauthCallback)
	return r
}

func setJsapiOauthMpConfig(t *testing.T) {
	t.Helper()
	operation_setting.WechatMpAppId = "wx-mp-test"
	operation_setting.WechatAppSecret = "mp-secret-test"
	t.Cleanup(func() {
		operation_setting.WechatMpAppId = ""
		operation_setting.WechatAppSecret = ""
	})
}

// 微信浏览器只能导航不能 POST，widget 侧必须靠 GET ?redirect=1 才能完成静默授权。
func TestWechatJsapiOauthStartNavigatesToAuthorize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setJsapiOauthMpConfig(t)

	w := httptest.NewRecorder()
	newJsapiOauthTestEngine().ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/user/wechat/jsapi/oauth/start?redirect=1&next=/agent", nil))

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "open.weixin.qq.com/connect/oauth2/authorize")
	assert.Contains(t, loc, "snsapi_base")
}

// 回跳目标只认 session，不认 URL：start 传入的站外 next 必须回落到钱包页，
// 否则这个回调就是开放重定向跳板。state 取自 start 的授权 URL，模拟真实往返。
func TestWechatJsapiOauthCallbackIgnoresUnsafeNext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setJsapiOauthMpConfig(t)
	engine := newJsapiOauthTestEngine()

	startW := httptest.NewRecorder()
	engine.ServeHTTP(startW, httptest.NewRequest(http.MethodGet,
		"/api/user/wechat/jsapi/oauth/start?redirect=1&next=https://evil.example/x", nil))
	require.Equal(t, http.StatusFound, startW.Code)
	startURL, err := url.Parse(startW.Header().Get("Location"))
	require.NoError(t, err)
	state := startURL.Query().Get("state")
	require.NotEmpty(t, state)

	callbackReq := httptest.NewRequest(http.MethodGet,
		"/api/user/wechat/jsapi/oauth/callback?state="+url.QueryEscape(state), nil)
	for _, ck := range startW.Result().Cookies() {
		callbackReq.AddCookie(ck)
	}
	callbackW := httptest.NewRecorder()
	engine.ServeHTTP(callbackW, callbackReq)

	require.Equal(t, http.StatusFound, callbackW.Code)
	assert.Equal(t, "/dashboard?wechat_jsapi=fail", callbackW.Header().Get("Location"))
}

// state 与 session 不符（伪造回调）→ 400，不给任何重定向。
func TestWechatJsapiOauthCallbackRejectsForgedState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setJsapiOauthMpConfig(t)

	w := httptest.NewRecorder()
	newJsapiOauthTestEngine().ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/user/wechat/jsapi/oauth/callback?state=forged&code=x", nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
}
