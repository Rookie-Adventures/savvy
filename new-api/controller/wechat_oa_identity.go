package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// 微信身份体系(服务号 OAuth)——跨设备扫码绑定/登录 + 微信内直登。
//
// 安全要点:
//   - state = "kind:token" 自含票证:token 为 crypto/rand 256bit,桌面与手机不共享
//     session,票证不可猜测即天然防 CSRF;
//   - callback 不接收任何用户可控 redirect,所有 302 目标均为硬编码本地路径;
//   - openid 占用时拒绝创建绑定(绝不覆盖既有绑定,规格红线)。
const (
	weChatOAProvider     = "oa" // 服务号;未来小程序/开放平台用独立 provider 值
	weChatOACallbackPath = "/api/wechat/oa/callback"
	weChatOAEntryPath    = "/api/wechat/oa/entry"
)

// ponytail: 换码函数抽成包级变量,测试注入 stub(真实换码需联网)。
var wechatExchangeCodeFn = service.ExchangeWechatOauthCode

type weChatOATokenRequest struct {
	Kind string `json:"kind"`
}

// CreateWeChatOAIdentityToken POST /api/wechat/oa/tokens(匿名)
// kind=login 供桌面渲染扫码 QR;kind=direct 供微信内直接跳转授权。
func CreateWeChatOAIdentityToken(c *gin.Context) {
	var req weChatOATokenRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的请求"})
		return
	}
	if req.Kind != "login" && req.Kind != "direct" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的 kind"})
		return
	}
	if !wechatOAConfigured() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "微信登录未配置"})
		return
	}
	tok, err := model.CreateWeChatOAuthToken(req.Kind, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建票证失败"})
		return
	}
	url := service.BuildWechatOauthAuthorizeURLFor(tok.Kind+":"+tok.Token, weChatOACallbackPath)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"token": tok.Token,
		"url":   url,
	}})
}

// weChatOASessionUserId 读登录用户 id:优先 session(bind 票证归属校验必须绕开中间件),
// 兜底 gin context(UserAuth 注入)。
func weChatOASessionUserId(c *gin.Context) int {
	if v := sessions.Default(c).Get("id"); v != nil {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return c.GetInt("id")
}

// CreateWeChatOABindToken POST /api/user/wechat/oa/tokens(selfRoute,登录态)
// kind=bind,票证归属当前用户;qr_url 为 entry 全链,供桌面渲染扫码 QR。
func CreateWeChatOABindToken(c *gin.Context) {
	userId := weChatOASessionUserId(c)
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "无权操作"})
		return
	}
	if !wechatOAConfigured() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "微信登录未配置"})
		return
	}
	tok, err := model.CreateWeChatOAuthToken("bind", userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建票证失败"})
		return
	}
	state := "bind:" + tok.Token
	entry := strings.TrimRight(service.GetCallbackAddress(), "/") + weChatOAEntryPath + "?state=" + strings.ReplaceAll(state, ":", "%3A")
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"token":  tok.Token,
		"qr_url": entry,
	}})
}

// GetWeChatOATokenStatus GET /api/wechat/oa/tokens/:token/status
// login/direct kind 无归属,票证即权限;bind kind 要求登录态且为票证归属者(否则 403)。
func GetWeChatOATokenStatus(c *gin.Context) {
	tok, err := model.GetWeChatOAuthTokenByToken(c.Param("token"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "票证不存在"})
		return
	}
	if tok.Kind == "bind" {
		userId := weChatOASessionUserId(c)
		if userId <= 0 || userId != tok.UserId {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权查看该票证"})
			return
		}
	}
	status := tok.Status
	if tok.Status == "pending" && tok.IsExpired() {
		status = "expired"
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"status":     status,
		"need_action": status == "authorized",
	}})
}

// WeChatOAEntry GET /api/wechat/oa/entry?state=kind:token(匿名,手机扫码/微信内直登入口)
// 校验票证 pending 未过期后 302 到微信授权页;非法/过期 → 400 文本页(不重定向,防 open-redirect)。
func WeChatOAEntry(c *gin.Context) {
	kind, token, ok := weChatOAStateKind(c.Query("state"))
	if !ok {
		wechatOATextPage(c, http.StatusBadRequest, "链接无效，请重新发起")
		return
	}
	tok, err := model.GetWeChatOAuthTokenByToken(token)
	if err != nil || tok.Kind != kind || tok.Status != "pending" || tok.IsExpired() {
		wechatOATextPage(c, http.StatusBadRequest, "链接已失效，请重新发起")
		return
	}
	if !wechatOAConfigured() {
		wechatOATextPage(c, http.StatusBadRequest, "微信登录未配置")
		return
	}
	c.Redirect(http.StatusFound, service.BuildWechatOauthAuthorizeURLFor(c.Query("state"), weChatOACallbackPath))
}

// WeChatOACallback GET /api/wechat/oa/callback?code&state(匿名,微信授权回跳)
// 换 openid → 按票证 kind 分支处理;bind/login 返极简中文 HTML(手机展示),
// direct 重定向目标全部为硬编码本地路径。
func WeChatOACallback(c *gin.Context) {
	kind, token, ok := weChatOAStateKind(c.Query("state"))
	if !ok {
		wechatOATextPage(c, http.StatusBadRequest, "链接无效，请重新发起")
		return
	}
	tok, err := model.GetWeChatOAuthTokenByToken(token)
	if err != nil || tok.Kind != kind || tok.Status != "pending" || tok.IsExpired() {
		wechatOATextPage(c, http.StatusBadRequest, "链接已失效，请重新发起")
		return
	}
	openid, err := wechatExchangeCodeFn(c.Request.Context(), c.Query("code"))
	if err != nil || openid == "" {
		// 票证保持 pending,用户可重新扫码重试
		wechatOATextPage(c, http.StatusBadRequest, "微信授权失败，请重新扫码")
		return
	}
	appId := operation_setting.WechatMpAppId

	switch kind {
	case "bind":
		wechatOABindCallback(c, tok, appId, openid)
	case "login":
		wechatOLoginCallback(c, tok, appId, openid)
	case "direct":
		wechatOADirectCallback(c, tok, appId, openid)
	default:
		wechatOATextPage(c, http.StatusBadRequest, "链接无效，请重新发起")
	}
}

// bind:openid 已占用 → rejected(绝不覆盖);否则创建绑定 → completed。
func wechatOABindCallback(c *gin.Context, tok *model.WeChatOAuthToken, appId, openid string) {
	_, err := model.GetWeChatAccountByOpenid(weChatOAProvider, appId, openid)
	if err == nil {
		_ = model.ConsumeWeChatOAuthToken(tok.Token, "rejected", 0, openid)
		wechatOATextPage(c, http.StatusOK, "该微信已绑定其他账户，无法重复绑定")
		return
	}
	err = model.CreateWeChatAccount(&model.WeChatAccount{
		UserId: tok.UserId, Provider: weChatOAProvider, AppId: appId, Openid: openid,
	})
	if err != nil {
		_ = model.ConsumeWeChatOAuthToken(tok.Token, "rejected", 0, openid)
		wechatOATextPage(c, http.StatusOK, "绑定失败：该微信已绑定其他账户")
		return
	}
	_ = model.ConsumeWeChatOAuthToken(tok.Token, "completed", 0, openid)
	wechatOATextPage(c, http.StatusOK, "绑定成功，请回到电脑查看")
}

// login:openid 已绑 → completed(桌面轮询后 claim 落会话);未绑 → authorized(openid 暂存)。
func wechatOLoginCallback(c *gin.Context, tok *model.WeChatOAuthToken, appId, openid string) {
	acc, err := model.GetWeChatAccountByOpenid(weChatOAProvider, appId, openid)
	if err == nil {
		_ = model.ConsumeWeChatOAuthToken(tok.Token, "completed", acc.UserId, "")
		wechatOATextPage(c, http.StatusOK, "登录成功，请回到电脑继续")
		return
	}
	_ = model.ConsumeWeChatOAuthToken(tok.Token, "authorized", 0, openid)
	wechatOATextPage(c, http.StatusOK, "请在电脑上选择「创建新账户」或「绑定已有账户」")
}

// direct(微信内直登,手机自身即登录设备):已绑 → 消费并本会话落地 → 302 /console/topup;
// 未绑 → authorized → 302 /sign-in?wx_token=<token>(前端亮两按钮)。
func wechatOADirectCallback(c *gin.Context, tok *model.WeChatOAuthToken, appId, openid string) {
	acc, err := model.GetWeChatAccountByOpenid(weChatOAProvider, appId, openid)
	if err == nil {
		if err := model.ConsumeWeChatOAuthToken(tok.Token, "consumed", acc.UserId, ""); err != nil {
			wechatOATextPage(c, http.StatusBadRequest, "链接已失效，请重新发起")
			return
		}
		user, err := model.GetUserById(acc.UserId, false)
		if err != nil {
			wechatOATextPage(c, http.StatusBadRequest, "账户异常，请联系管理员")
			return
		}
		wechatOADirectLogin(user, c)
		c.Redirect(http.StatusFound, "/console/topup")
		return
	}
	if err := model.ConsumeWeChatOAuthToken(tok.Token, "authorized", 0, openid); err != nil {
		wechatOATextPage(c, http.StatusBadRequest, "链接已失效，请重新发起")
		return
	}
	c.Redirect(http.StatusFound, "/sign-in?wx_token="+tok.Token)
}

// wechatOADirectLogin 会话落地+审计。
// ponytail: 不直接复用 setupLogin —— 它以 c.JSON 收尾,与 direct 场景所需的 302 冲突
// (先写 200 JSON 再 302 会 header 冲突);此处仅复刻其 session 字段集合,不改原函数
// (铁律 2:不重构现有认证)。
func wechatOADirectLogin(user *model.User, c *gin.Context) {
	session := sessions.Default(c)
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("group", user.Group)
	if err := session.Save(); err != nil {
		common.SysError("wechat oa direct login session save failed: " + err.Error())
	}
	model.UpdateUserLastLoginAt(user.Id)
	model.RecordLoginLog(user.Id, user.Username, "Logged in successfully via wechat_oa_direct",
		c.ClientIP(), "login", map[string]interface{}{"method": "wechat_oa_direct"}, nil)
}

func weChatOAStateKind(state string) (string, string, bool) {
	kind, token, ok := strings.Cut(state, ":")
	if !ok || kind == "" || token == "" {
		return "", "", false
	}
	return kind, token, true
}

func wechatOAConfigured() bool {
	return operation_setting.WechatMpAppId != "" && operation_setting.WechatAppSecret != ""
}

// wechatOATextPage 极简中文文本页:手机微信内展示回调结果,无模板依赖。
func wechatOATextPage(c *gin.Context, status int, msg string) {
	c.Data(status, "text/html; charset=utf-8", []byte(
		`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="font-family:sans-serif;padding:2em;text-align:center"><p>`+
			msg+`</p></body></html>`))
}
