package service

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 服务号消息网关原语：签名校验 / access_token 缓存 / 客服消息下发 / 入站 XML 解析。
// 这些均为微信公众平台标准接口，不触碰支付核心；支付的 JSAPI 预下单在 controller/wechat_mp.go。

const (
	wechatMpTokenURL      = "https://api.weixin.qq.com/cgi-bin/token"
	wechatMpCustomSendURL = "https://api.weixin.qq.com/cgi-bin/message/custom/send"
	mpAccessTokenTTL      = 7000 * time.Second // 微信 access_token 7200s 过期,留 200s 余量
)

// mpTokenCache 缓存服务号 access_token(客服消息下发需要)。微信限频获取,故进程内缓存。
var (
	mpTokenCache     string
	mpTokenExpireAt  time.Time
	mpTokenCacheMu   sync.Mutex
)

// IsWechatMpMessageConfigured 报告服务号消息网关是否具备收发能力(消息校验 Token + 静默授权同号凭据)。
func IsWechatMpMessageConfigured() bool {
	return operation_setting.WechatMpToken != "" &&
		operation_setting.WechatMpAppId != "" &&
		operation_setting.WechatAppSecret != ""
}

// VerifyMpSignature 校验服务号回调签名:signature = SHA1(sorted(token, timestamp, nonce) 拼接)。
// 微信 GET 握手与 POST 消息均用同一算法(Encrypt 模式另需 msg_signature,本方案先支持明文/安全模式基础验签)。
func VerifyMpSignature(token, signature, timestamp, nonce string) bool {
	if token == "" || signature == "" {
		return false
	}
	items := []string{token, timestamp, nonce}
	sort.Strings(items)
	raw := strings.Join(items, "")
	sum := sha1.Sum([]byte(raw))
	computed := fmt.Sprintf("%x", sum)
	return computed == signature
}

// getMpAccessToken 获取服务号 access_token(客服消息下发凭证),进程内缓存避免频繁拉取(微信限频)。
func getMpAccessToken(ctx context.Context) (string, error) {
	mpTokenCacheMu.Lock()
	defer mpTokenCacheMu.Unlock()
	if mpTokenCache != "" && time.Now().Before(mpTokenExpireAt) {
		return mpTokenCache, nil
	}
	if operation_setting.WechatMpAppId == "" || operation_setting.WechatAppSecret == "" {
		return "", fmt.Errorf("wechat mp appid/secret not configured")
	}
	u := fmt.Sprintf("%s?grant_type=client_credential&appid=%s&secret=%s",
		wechatMpTokenURL,
		url.QueryEscape(operation_setting.WechatMpAppId),
		url.QueryEscape(operation_setting.WechatAppSecret),
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
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := common.DecodeJson(resp.Body, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 || out.AccessToken == "" {
		return "", fmt.Errorf("wechat mp token failed: errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
	}
	ttl := mpAccessTokenTTL
	if out.ExpiresIn > 0 {
		ttl = time.Duration(out.ExpiresIn-200) * time.Second
	}
	mpTokenCache = out.AccessToken
	mpTokenExpireAt = time.Now().Add(ttl)
	return mpTokenCache, nil
}

// SendCustomTextMessage 经客服消息接口向 openid 推送文本(用于 5s 超时后的异步回推)。
func SendCustomTextMessage(ctx context.Context, openid, text string) error {
	body := map[string]any{
		"touser":  openid,
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	}
	return sendCustomMessage(ctx, body)
}

// SendCustomNewsMessage 经客服消息接口推送图文(用于充值:把 JSAPI 支付 H5 页以图文链接带出)。
func SendCustomNewsMessage(ctx context.Context, openid string, articles []MpNewsArticle) error {
	body := map[string]any{
		"touser":  openid,
		"msgtype": "news",
		"news":    map[string]any{"articles": articles},
	}
	return sendCustomMessage(ctx, body)
}

// MpNewsArticle 客服消息图文条目。
type MpNewsArticle struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	PicURL      string `json:"picurl,omitempty"`
}

func sendCustomMessage(ctx context.Context, body map[string]any) error {
	token, err := getMpAccessToken(ctx)
	if err != nil {
		return err
	}
	jsonBytes, err := common.Marshal(body)
	if err != nil {
		return err
	}
	u := wechatMpCustomSendURL + "?access_token=" + url.QueryEscape(token)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := common.DecodeJson(resp.Body, &out); err != nil {
		return err
	}
	if out.ErrCode != 0 {
		return fmt.Errorf("wechat custom message failed: errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
	}
	return nil
}

// MpInboundMessage 服务号入站消息(标准 XML)。仅取网关所需字段。
type MpInboundMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"` // 即用户 openid
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgId        int64    `xml:"MsgId"`
	Event        string   `xml:"Event"`
	EventKey     string   `xml:"EventKey"`
}

// ParseInboundMpMessage 解析微信推送的 XML 消息体。
func ParseInboundMpMessage(b []byte) (*MpInboundMessage, error) {
	var m MpInboundMessage
	if err := xml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// 注:服务号消息的"大脑"已由 controller/wechat_mp.go 的 routeMpIntent 确定性路由接管,
// 不再调用 Hermes agent。重型 agent 每条消息都会重载完整 system context + 工具定义,
// 对"充值/查余额/查订单"这类有限意图属于严重过度调用, token 成本不可接受。
// 零 token 规则路由即可覆盖 savvy-quota-topup 的 6 个意图;账户类意图在 openid 绑定
// (wechat-identity-phase1) 上线后直接调用能力层, 同样无需 LLM。
