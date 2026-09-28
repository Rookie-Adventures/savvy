package controller

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const wechatJsapiOpenidSessionKey = "wechat_jsapi_openid"
const wechatJsapiStateSessionKey = "wechat_jsapi_oauth_state"
const wechatJsapiNextSessionKey = "wechat_jsapi_next"

const wechatJsapiDefaultNext = "/dashboard"

// WechatJsapiOauthStart:生成 state(防 CSRF)存 session,返回服务号授权 URL。
// 前端拿到后 window.location.href 跳转,微信带 code 回 callback。
//
// 两种用法同一个处理器:
//   - POST(钱包页):返回 JSON,前端自己跳。
//   - GET ?next=/agent&redirect=1(widget/真机导航):直接 302 去授权页。微信浏览器里
//     只能导航不能 POST,没有这个模式游客就无法完成静默授权。
//
// next 只在这里(同源、经 IsSafeLocalRedirect 校验)收下并存进 session;callback 侧
// 绝不从 URL 读取回跳目标,因此外部无法把用户导向站外。
func WechatJsapiOauthStart(c *gin.Context) {
	if operation_setting.WechatMpAppId == "" || operation_setting.WechatAppSecret == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "微信支付未配置"})
		return
	}
	next := c.Query("next")
	if next == "" {
		next = wechatJsapiDefaultNext
	}
	if !service.IsSafeLocalRedirect(next) {
		next = wechatJsapiDefaultNext
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "生成 state 失败"})
		return
	}
	state := hex.EncodeToString(buf)
	session := sessions.Default(c)
	session.Set(wechatJsapiStateSessionKey, state)
	session.Set(wechatJsapiNextSessionKey, next)
	_ = session.Save()
	authorizeURL := service.BuildWechatOauthAuthorizeURL(state)
	if c.Request.Method == http.MethodGet && c.Query("redirect") == "1" {
		c.Redirect(http.StatusFound, authorizeURL)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    gin.H{"authorize_url": authorizeURL},
	})
}

// WechatJsapiOauthCallback:校验 state(防 CSRF)、换 openid、存 session、302 回 start 时记下的 next。
// ponytail: 回跳地址取自 session 而非 query,并对 session 值二次 IsSafeLocalRedirect——
// session 内容也可能来自旧版本/被其他代码写入,不重复信任。
func WechatJsapiOauthCallback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")
	session := sessions.Default(c)
	saved := session.Get(wechatJsapiStateSessionKey)
	if state == "" || saved == nil || saved.(string) != state {
		c.JSON(http.StatusBadRequest, gin.H{"message": "error", "data": "state 校验失败"})
		return
	}
	// 无论成败先取走 next 并清 state,避免一次失败后旧 state 反复被复用
	back, _ := session.Get(wechatJsapiNextSessionKey).(string)
	if back == "" || !service.IsSafeLocalRedirect(back) {
		back = wechatJsapiDefaultNext
	}
	session.Delete(wechatJsapiStateSessionKey)
	session.Delete(wechatJsapiNextSessionKey)
	if code == "" {
		_ = session.Save()
		c.Redirect(http.StatusFound, back+"?wechat_jsapi=fail")
		return
	}
	openid, err := service.ExchangeWechatOauthCode(c.Request.Context(), code)
	if err != nil {
		_ = session.Save()
		c.Redirect(http.StatusFound, back+"?wechat_jsapi=fail")
		return
	}
	session.Set(wechatJsapiOpenidSessionKey, openid)
	_ = session.Save()
	c.Redirect(http.StatusFound, back+"?wechat_jsapi=1")
}
