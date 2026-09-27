package controller

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
)

// 服务号消息网关：GET 握手校验 + POST 收消息(验签→即时被动回复→异步意图路由→客服消息回推)。
// 与 web 钱包 JSAPI 不同,此处 openid 直接来自微信推送的 FromUserName,无需 OAuth。
// 路由需注册为匿名(签名即鉴权),对齐 WechatJsapiOauthCallback 的匿名范式。
//
// 重要:本网关**不调用任何重型 agent**(Hermes / Bailian)。savvy-quota-topup 已把意图收敛为
// 6 个有限动作,用纯规则路由即可零 token 命中;账户类意图在 openid 绑定上线后直连能力层。

// WechatMpMessageGet 服务号回调握手:校验 signature 后原样返回 echostr。
func WechatMpMessageGet(c *gin.Context) {
	signature := c.Query("signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	echostr := c.Query("echostr")
	if !service.VerifyMpSignature(operation_setting.WechatMpToken, signature, timestamp, nonce) {
		c.String(http.StatusForbidden, "invalid signature")
		return
	}
	c.String(http.StatusOK, echostr)
}

// WechatMpMessagePost 服务号推送消息:验签→解析→5s 内返回被动回复→异步处理并客服消息回推。
func WechatMpMessagePost(c *gin.Context) {
	signature := c.Query("signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	if !service.VerifyMpSignature(operation_setting.WechatMpToken, signature, timestamp, nonce) {
		c.String(http.StatusForbidden, "invalid signature")
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		c.String(http.StatusBadRequest, "bad body")
		return
	}
	msg, err := service.ParseInboundMpMessage(body)
	if err != nil {
		c.String(http.StatusBadRequest, "bad xml")
		return
	}
	// 即时被动回复,避免微信 5s 超时重试(真正内容走客服消息异步回推)。
	c.String(http.StatusOK, passiveReplyXML(msg.FromUserName, msg.ToUserName, "正在处理，请稍候…"))

	// 仅处理文本消息;事件(关注/菜单)暂只 ack,后续扩展。
	if msg.MsgType != "text" || strings.TrimSpace(msg.Content) == "" {
		return
	}
	openid := msg.FromUserName
	userText := strings.TrimSpace(msg.Content)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		mpHandleMessage(ctx, openid, userText)
	}()
}

// mpIntent 枚举服务号可处理的有限意图(零 LLM,纯规则路由,对齐 savvy-quota-topup)。
type mpIntent int

const (
	mpIntentUnknown mpIntent = iota
	mpIntentTopUp
	mpIntentBalance
	mpIntentUsage
	mpIntentOrders
	mpIntentRedeem
	mpIntentRefund
)

// mpRoute 意图路由结果:意图 + 抽取出的槽位。
type mpRoute struct {
	intent mpIntent
	amount float64 // topup:金额(元)
	days   int     // usage:天数
}

// routeMpIntent 确定性意图识别,覆盖 savvy-quota-topup 的 6 个有限意图,不调用任何 LLM。
// 退款/兑换/订单/用量/余额 按关键词优先级靠前匹配;充值需含明确动作词 + (可选)金额。
// 均未命中 → unknown(回退菜单,零 token)。
func routeMpIntent(text string) mpRoute {
	t := strings.TrimSpace(text)
	lower := strings.ToLower(t)

	switch {
	case containsAny(t, "退款", "退一下", "退钱", "误充", "退掉", "申请退款"):
		return mpRoute{intent: mpIntentRefund}
	case containsAny(t, "兑换", "兑换码", "优惠码", "激活码") || strings.Contains(lower, "cdk"):
		return mpRoute{intent: mpIntentRedeem}
	case containsAny(t, "订单", "充值记录", "我的单", "账单", "到账没", "那笔", "记录"):
		return mpRoute{intent: mpIntentOrders}
	case containsAny(t, "用量", "用了多少", "花了多少", "消耗", "消费", "近几天", "最近花"):
		return mpRoute{intent: mpIntentUsage, days: extractDays(t)}
	case containsAny(t, "余额", "还剩", "剩余", "额度多少", "还有多少", "账户余额", "我有多少"):
		return mpRoute{intent: mpIntentBalance}
	case containsAny(t, "充值", "买额度", "充额度", "加额度", "续费", "充钱", "充点"):
		if amt, ok := extractAmount(t); ok {
			return mpRoute{intent: mpIntentTopUp, amount: amt}
		}
		return mpRoute{intent: mpIntentTopUp} // 无金额:引导用户输入金额
	}
	return mpRoute{intent: mpIntentUnknown}
}

// mpHandleMessage 异步处理单条服务号消息:确定性意图路由 → 充值 or 引导话术,经客服消息回推。
// 全程零 LLM 调用。
func mpHandleMessage(ctx context.Context, openid, userText string) {
	route := routeMpIntent(userText)
	switch route.intent {
	case mpIntentTopUp:
		if route.amount <= 0 {
			_ = service.SendCustomTextMessage(ctx, openid, "好的，请告诉我充值金额（例如：充值 50 元）。")
			return
		}
		data, err := CreateAgentMpJsapiTopUp(openid, route.amount)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("mp jsapi topup failed: openid=%s err=%v", openid, err))
			_ = service.SendCustomTextMessage(ctx, openid, "充值下单失败，请稍后重试或联系客服。")
			return
		}
		article := service.MpNewsArticle{
			Title:       fmt.Sprintf("微信充值 ¥%.2f", route.amount),
			Description: "点击进入微信内支付，完成即到账。",
			URL:         data["pay_url"].(string),
		}
		if err := service.SendCustomNewsMessage(ctx, openid, []service.MpNewsArticle{article}); err != nil {
			logger.LogError(ctx, fmt.Sprintf("mp send topup news failed: %v", err))
		}
	case mpIntentBalance, mpIntentUsage, mpIntentOrders, mpIntentRedeem, mpIntentRefund:
		// 账户类意图需 openid 绑定 Savvy 账号(wechat-identity-phase1 已测未部署)。
		// v1:确定性引导话术,零 token。绑定上线后此处注入 openid→user_id→能力层调用。
		_ = service.SendCustomTextMessage(ctx, openid, mpAccountIntentGuide(route.intent))
	default:
		_ = service.SendCustomTextMessage(ctx, openid, mpHelpText())
	}
}

// mpAccountIntentGuide 账户类意图的确定性引导(绑定前的 v1 行为,零 token)。
func mpAccountIntentGuide(intent mpIntent) string {
	switch intent {
	case mpIntentBalance:
		return "查余额需先在 Savvy 控制台绑定微信账号，绑定后我直接帮你查。"
	case mpIntentUsage:
		return "查用量需先绑定 Savvy 账号，绑定后我直接帮你拉取近 7 天消耗。"
	case mpIntentOrders:
		return "查充值订单需先绑定 Savvy 账号，绑定后我直接帮你列出来。"
	case mpIntentRedeem:
		return "兑换优惠码需先绑定 Savvy 账号，绑定后把兑换码发我即可。"
	case mpIntentRefund:
		return "申请退款需先绑定 Savvy 账号，绑定后把订单号发我即可。"
	}
	return mpHelpText()
}

// mpHelpText 未命中任何已知意图时的菜单(零 token)。
func mpHelpText() string {
	return "你好，我是 Savvy 额度助手。可以：\n" +
		"• 充值：发「充值 50」\n" +
		"• 查余额 / 查用量 / 查订单 / 兑换码 / 退款（需先绑定 Savvy 账号）"
}

// containsAny 任一子串命中即返回 true。
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// extractAmount 从文本抽取金额(元),支持小数(最多两位)。
func extractAmount(text string) (float64, bool) {
	re := regexp.MustCompile(`(\d+(?:\.\d{1,2})?)`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// extractDays 从用量查询文本抽取天数,默认 7(服务端会钳制到 1~30)。
func extractDays(text string) int {
	if containsAny(text, "周") {
		return 7
	}
	if containsAny(text, "月", "30天") {
		return 30
	}
	if m := regexp.MustCompile(`(\d+)\s*天`).FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 30 {
			return n
		}
	}
	return 7
}

// CreateAgentMpJsapiTopUp agent 触发、带 openid 的 JSAPI 充值:建单 + JSAPI 预下单 + 返回 H5 支付页 URL。
// 与服务号内原生体验对应:用户在服务号点图文→进 H5 页→WeixinJSBridge 调起支付 sheet。
func CreateAgentMpJsapiTopUp(openid string, amountYuan float64) (map[string]any, error) {
	totalCents, ok := agentTopUpAmountCents(amountYuan)
	if !ok {
		return nil, fmt.Errorf("amount out of range")
	}
	svc := GetWechatJsapiClient()
	if svc == nil {
		return nil, fmt.Errorf("wechat jsapi client not configured")
	}
	claimToken, err := newClaimToken()
	if err != nil {
		return nil, err
	}
	outTradeNo := fmt.Sprintf("WXAGT%s%s", time.Now().Format("20060102150405"), common.GetRandomString(10))
	userId := 0
	// 该微信已绑账户(此前认领过)→ 直接开登录单,notify 命中现成自动入账路径,无需再认领
	if uid, bound := model.GetUserIdByMpOpenid(openid); bound {
		userId = uid
	}
	topUp := &model.TopUp{
		UserId:          userId, // 0=服务号游客单,付款后凭 claim_token 认领并绑定
		TradeNo:         outTradeNo,
		ClaimToken:      claimToken,
		Money:           amountYuan,
		PaymentMethod:   model.PaymentMethodWechat,
		PaymentProvider: model.PaymentProviderWechatAgent,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		return nil, fmt.Errorf("create order failed: %w", err)
	}
	resp, _, err := svc.PrepayWithRequestPayment(context.Background(), jsapi.PrepayRequest{
		Appid:       core.String(operation_setting.WechatMpAppId),
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String("栗橙科技-服务号充值"),
		OutTradeNo:  core.String(outTradeNo),
		NotifyUrl:   core.String(service.GetCallbackAddress() + "/api/user/wechat/notify"),
		Amount: &jsapi.Amount{
			Total:    core.Int64(totalCents),
			Currency: core.String("CNY"),
		},
		Payer: &jsapi.Payer{Openid: core.String(openid)},
	})
	if err != nil {
		_ = model.UpdatePendingTopUpStatus(outTradeNo, model.PaymentProviderWechatAgent, common.TopUpStatusFailed)
		return nil, fmt.Errorf("jsapi prepay failed: %w", err)
	}
	// H5 支付页地址:页内凭 token 向后端取 JSAPI 调起参数。路由注册在 /api/mp/pay,
	// 裸 /mp/pay 会被 SPA 兜底成首页(200 假象),必须带 /api 前缀。
	payURL := strings.TrimRight(system_setting.ServerAddress, "/") + "/api/mp/pay?token=" + claimToken
	data := map[string]any{
		"out_trade_no": outTradeNo,
		"amount_yuan":  amountYuan,
		"claim_token":  claimToken,
		"pay_url":      payURL,
		"appId":        *resp.Appid,
		"timeStamp":    *resp.TimeStamp,
		"nonceStr":     *resp.NonceStr,
		"package":      *resp.Package,
		"signType":     *resp.SignType,
		"paySign":      *resp.PaySign,
	}
	// 缓存 JSAPI 调起参数,供 /mp/pay 页凭 claimToken 取回(P2 简化:进程内临时缓存)。
	mpPayCache.Store(claimToken, &mpPayPayload{
		AppId:      *resp.Appid,
		TimeStamp:  *resp.TimeStamp,
		NonceStr:   *resp.NonceStr,
		Package:    *resp.Package,
		SignType:   *resp.SignType,
		PaySign:    *resp.PaySign,
		AmountYuan: amountYuan,
		ExpireAt:   time.Now().Add(30 * time.Minute).Unix(),
	})
	return data, nil
}

// passiveReplyXML 构造微信被动回复 XML(文本)。ToUserName/FromUserName 需对调。
func passiveReplyXML(toUser, fromUser, content string) string {
	return fmt.Sprintf(
		"<xml><ToUserName><![CDATA[%s]]></ToUserName><FromUserName><![CDATA[%s]]></FromUserName><CreateTime>%d</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[%s]]></Content></xml>",
		toUser, fromUser, time.Now().Unix(), content,
	)
}

// ===== 服务号内 JSAPI 支付触发页(/mp/pay) =====

// mpPayPayload 服务号 JSAPI 调起参数(来自微信支付 prepay 返回值),凭 claimToken 临时缓存供 /mp/pay 页取回。
// P2 简化:进程内 sync.Map;多副本/重启场景需改用 Redis 或 DB(并加过期清理),否则缓存丢失后用户需重新发起充值。
type mpPayPayload struct {
	AppId      string  `json:"appId"`
	TimeStamp  string  `json:"timeStamp"`
	NonceStr   string  `json:"nonceStr"`
	Package    string  `json:"package"`
	SignType   string  `json:"signType"`
	PaySign    string  `json:"paySign"`
	AmountYuan float64 `json:"amountYuan"`
	ExpireAt   int64   `json:"-"`
}

// mpPayCache claimToken -> *mpPayPayload 的进程内临时缓存。
var mpPayCache sync.Map

// mpPayPageTpl 微信内支付页;第一个 %.2f 为金额,第二个 %s 为注入的 JSON 参数(后端签名受控值,非用户输入)。
const mpPayPageTpl = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<title>微信充值</title>
<style>
 body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;margin:0;background:#f5f5f5;color:#222}
 .card{max-width:420px;margin:16vh auto;background:#fff;border-radius:14px;padding:30px 22px;text-align:center;box-shadow:0 6px 24px rgba(0,0,0,.06)}
 .label{font-size:14px;color:#888}
 .amt{font-size:36px;font-weight:700;margin:8px 0 6px}
 .sub{color:#999;font-size:13px;margin-bottom:24px}
 button{width:100%%;padding:13px;border:0;border-radius:10px;background:#07c160;color:#fff;font-size:16px;font-weight:600}
 button:active{background:#06ad56}
 .tip{margin-top:16px;font-size:13px;color:#999;min-height:18px}
</style>
</head>
<body>
 <div class="card">
  <div class="label">微信充值</div>
  <div class="amt">¥%.2f</div>
  <div class="sub">支付完成后,本聊天会推送到账或认领消息</div>
  <button id="payBtn">立即支付</button>
  <div class="tip" id="tip"></div>
 </div>
<script>
 var PAY=%s;
 function invokePay(){
   var tip=document.getElementById('tip');
   function call(){
     WeixinJSBridge.invoke('getBrandWCPayRequest', PAY, function(r){
       if(r.err_msg==='get_brand_wcpay_request:ok'){ tip.textContent='支付成功,结果稍后在本聊天推送'; }
       else { tip.textContent='支付未完成,可在服务号重新发起'; }
     });
   }
   if(typeof WeixinJSBridge==='undefined'){
     document.addEventListener('WeixinJSBridgeReady', call, false);
     tip.textContent='正在唤起微信支付…';
   } else { call(); }
 }
 document.getElementById('payBtn').addEventListener('click', invokePay);
 window.addEventListener('load', function(){ setTimeout(invokePay, 300); });
</script>
</body>
</html>`

// WechatMpPayPage 服务号内 JSAPI 支付触发页:微信 webview 内凭 token 取回参数,调起微信原生支付面板。
func WechatMpPayPage(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		wechatMpPayError(c, "缺少支付凭证")
		return
	}
	val, ok := mpPayCache.Load(token)
	if !ok {
		wechatMpPayError(c, "支付凭证无效或已过期,请重新在服务号发起充值")
		return
	}
	p, ok := val.(*mpPayPayload)
	if !ok || p.ExpireAt < time.Now().Unix() {
		mpPayCache.Delete(token)
		wechatMpPayError(c, "支付凭证已过期,请重新在服务号发起充值")
		return
	}
	// 走 common.Marshal(项目 JSON 红线)。它默认转义 <,>,& —— 对 `var PAY=%s;` 这种
	// 内联 JS 字面量是等价的(\u003c 在 JS 里就是 <),且天然挡掉 </script> 提前闭合。
	payBytes, err := common.Marshal(p)
	if err != nil {
		wechatMpPayError(c, "支付参数处理失败")
		return
	}
	payJSON := string(payBytes)
	html := fmt.Sprintf(mpPayPageTpl, p.AmountYuan, payJSON)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// mpTopUpReply 服务号 JSAPI 充值入账后的聊天回推文案(纯函数可测)。
// 空串 = 不该推送(未成功/无认领凭据的中间态)。
func mpTopUpReply(tu *model.TopUp) string {
	if tu == nil || tu.Status != common.TopUpStatusSuccess {
		return ""
	}
	if tu.UserId > 0 {
		return fmt.Sprintf("✅ ¥%.2f 已到账,可在「我的钱包」查看余额。", tu.Money)
	}
	// 游客单:钱已收到,把认领入口送到用户眼前(claim_token 已在单上,不经模型/前端编造)
	if tu.ClaimToken == "" {
		return ""
	}
	return fmt.Sprintf("✅ ¥%.2f 已收到。首次在服务号充值,点开下面链接登录/注册并认领即可入账:%s/agent?claim_token=%s",
		tu.Money, strings.TrimRight(system_setting.ServerAddress, "/"), tu.ClaimToken)
}

// mpPushPaidTopUp 支付确认后经客服消息异步回推结果;失败仅记日志(客服兜底),不阻塞 notify 应答。
func mpPushPaidTopUp(openid, tradeNo string) {
	text := mpTopUpReply(model.GetTopUpByTradeNo(tradeNo))
	if text == "" || openid == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := service.SendCustomTextMessage(ctx, openid, text); err != nil {
			common.SysError("mp topup result push failed: " + err.Error())
		}
	}()
}

// wechatMpPayError 极简错误页(服务号 webview 内展示)。
func wechatMpPayError(c *gin.Context, msg string) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(
		`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="font-family:sans-serif;padding:2em;text-align:center;color:#666"><p>`+
			msg+`</p></body></html>`))
}
