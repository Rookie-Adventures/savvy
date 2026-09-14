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

// WechatJsapiOauthStart:登录态下生成 state(防 CSRF)存 session,返回服务号授权 URL。
// 前端拿到后 window.location.href 跳转,微信带 code 回 callback。
func WechatJsapiOauthStart(c *gin.Context) {
	if operation_setting.WechatMpAppId == "" || operation_setting.WechatAppSecret == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "微信支付未配置"})
		return
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "生成 state 失败"})
		return
	}
	state := hex.EncodeToString(buf)
	session := sessions.Default(c)
	session.Set(wechatJsapiStateSessionKey, state)
	_ = session.Save()
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    gin.H{"authorize_url": service.BuildWechatOauthAuthorizeURL(state)},
	})
}

// WechatJsapiOauthCallback:校验 state(防 CSRF+open-redirect)、换 openid、存 session、302 回前端钱包页。
// ponytail: 最终回跳地址固定为本地钱包页(用 IsSafeLocalRedirect 二次保险),绝不信任任何外部 redirect 参数。
func WechatJsapiOauthCallback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")
	session := sessions.Default(c)
	saved := session.Get(wechatJsapiStateSessionKey)
	if state == "" || saved == nil || saved.(string) != state {
		c.JSON(http.StatusBadRequest, gin.H{"message": "error", "data": "state 校验失败"})
		return
	}
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "error", "data": "缺少 code"})
		return
	}
	openid, err := service.ExchangeWechatOauthCode(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "error", "data": "换取 openid 失败"})
		return
	}
	session.Set(wechatJsapiOpenidSessionKey, openid)
	session.Delete(wechatJsapiStateSessionKey)
	_ = session.Save()
	// 固定回跳本地钱包页(同源),不为外部控制。
	back := "/dashboard"
	if !service.IsSafeLocalRedirect(back) {
		back = "/dashboard"
	}
	c.Redirect(http.StatusFound, back+"?wechat_jsapi=1")
}
