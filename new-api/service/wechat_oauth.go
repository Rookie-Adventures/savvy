package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const wechatOauthTokenURL = "https://api.weixin.qq.com/sns/oauth2/access_token"

// 静默授权回调落地页(同域),微信带 code 跳回这里,换到 openid 后 302 回前端钱包页。
// ponytail: redirect_uri 必须与应用号后台配置的授权回调域名一致,且此处 URL-encode。
func wechatJsapiOauthCallbackPath() string {
	return "/api/user/wechat/jsapi/oauth/callback"
}

// BuildWechatOauthAuthorizeURL 构造服务号 snsapi_base 静默授权 URL。
// state 用于防 CSRF(回调时校验);redirect_uri 固定为本服务回调路径(同源,非外部可控)。
func BuildWechatOauthAuthorizeURL(state string) string {
	base := strings.TrimRight(GetCallbackAddress(), "/") // ponytail: 用 new-api 自身对外地址,对齐 service.GetCallbackAddress 范式
	redirect := base + wechatJsapiOauthCallbackPath()
	return fmt.Sprintf(
		"https://open.weixin.qq.com/connect/oauth2/authorize?appid=%s&redirect_uri=%s&response_type=code&scope=snsapi_base&state=%s#wechat_redirect",
		url.QueryEscape(operation_setting.WechatMpAppId), // 服务号 AppID(openid 按账号隔离)
		url.QueryEscape(redirect),
		url.QueryEscape(state),
	)
}

// ExchangeWechatOauthCode 用 code 换 openid(snsapi_base 只返 openid+token,不弹授权页)。
// ponytail: 失败不静默——返 err,controller 据 err 决定重定向到错误页而非假装成功。
func ExchangeWechatOauthCode(ctx context.Context, code string) (string, error) {
	if operation_setting.WechatAppSecret == "" {
		return "", fmt.Errorf("wechat app secret not configured")
	}
	u := fmt.Sprintf(
		"%s?appid=%s&secret=%s&code=%s&grant_type=authorization_code",
		wechatOauthTokenURL,
		url.QueryEscape(operation_setting.WechatMpAppId),
		url.QueryEscape(operation_setting.WechatAppSecret),
		url.QueryEscape(code),
	)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Openid  string `json:"openid"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := common.DecodeJson(resp.Body, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 || out.Openid == "" {
		return "", fmt.Errorf("wechat oauth exchange failed: errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
	}
	return out.Openid, nil
}

// IsSafeLocalRedirect 防 open-redirect:仅允许同源相对路径(以 / 开头、非 // 或 \ 开头、不含 scheme)。
// 额外拦截 %2f/%5c 编码绕过的双斜杠(open-redirect 常见 bypass)。
func IsSafeLocalRedirect(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "//") || strings.HasPrefix(path, "\\") {
		return false
	}
	if strings.Contains(path, "://") {
		return false
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return false
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	return true
}
