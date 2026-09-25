package controller

// 微信 AI 支付(X402 / Pay Skill)商户侧接口。
//
// 协议九步在本仓的分工:
//
//	① Agent POST /api/x402/invoke                      → SkillInvoke
//	② Native 下单(复用 GetWechatClient,现有商户证书)  → SkillInvoke 首单分支
//	③ AI 预下单(SkillHub 开发者密钥)                    → service.X402Preorder
//	④ 402 + WeixinPay-Required + X-Out-Trade-No          → SkillInvoke 首单分支
//	⑤⑥ Agent 调 weixinpay_pay,用户手机授权扣款         → (微信侧,本仓不参与)
//	⑦ Agent 携 X-Out-Trade-No 重试                      → SkillInvoke 重试分支
//	⑧ 查单验证 trade_state=SUCCESS                      → QueryOrderByOutTradeNo
//	⑨ 200 + 付费内容(身份已确认=入账;未确认=挂账)     → settleX402
//
// 身份策略(与产品约定一致):钱跟着订单走。首单不要求身份;支付完成后若该
// agent 身份已绑定过 savvy 账号则直接入账,否则入 held 队列,等用户用微信
// 打开 claim 链接 —— 走现有 /api/oauth/wechat 登录/注册体系(服务号内自动
// 登录、未注册自动建号),绑定完成瞬间队列排空。15 分钟未绑定 → expired,
// 因订单未真实扣款或可退款,无资金损失。

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

const x402TradeNoPrefix = "WX402_"

type x402InvokeRequest struct {
	Query       string `json:"query"`
	PaymentCode string `json:"payment_code"` // 兜底:部分 Agent 只回传 body,不回传 header
}

// SkillInvoke — 单接口承载两种场景,以 X-Out-Trade-No 请求头区分(协议规定)。
func SkillInvoke(c *gin.Context) {
	if !operation_setting.IsX402Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "X402_NOT_CONFIGURED", "message": "微信AI支付未启用"})
		return
	}
	var req x402InvokeRequest
	_ = c.ShouldBindJSON(&req) // query 允许为空,不作为下单前置

	headerOf := func(name string) string {
		for k, v := range c.Request.Header {
			if strings.EqualFold(k, name) && len(v) > 0 {
				return v[0]
			}
		}
		return ""
	}
	outTradeNo := strings.TrimSpace(headerOf("X-Out-Trade-No"))
	agentUserId := strings.TrimSpace(headerOf("X-Agent-User-Id"))

	if outTradeNo == "" {
		skillFirstRequest(c, req.Query, agentUserId)
		return
	}
	// 协议规定 Agent 重试必须同时回显两个首单响应头,优先取头;body 兜底仅覆盖
	// 「只读 body 的 Agent」,两处都没有就当未授权(不查单、不入账)。
	code := headerOf("WeixinPay-Required")
	if code == "" {
		code = strings.TrimSpace(req.PaymentCode)
	}
	skillRetry(c, req.Query, agentUserId, outTradeNo, code)
}

// skillFirstRequest — ②③④:Native 下单 → 预下单 → 402。
//
// 失败即关单(不留未支付脏单);金额取 X402AmountCents(单一 SKU 口径)。
func skillFirstRequest(c *gin.Context, query string, agentUserId string) {
	svc := GetWechatClient()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "WECHAT_NOT_CONFIGURED", "message": "微信支付商户证书未配置,无法下单"})
		return
	}
	tradeNo := service.X402OutTradeNo()
	amountCents := operation_setting.X402AmountCents
	callbackBase := service.GetCallbackAddress()
	resp, _, err := svc.Prepay(c.Request.Context(), native.PrepayRequest{
		Appid:       core.String(operation_setting.WechatAppId),
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String(operation_setting.X402ServiceName + " Pay Skill 调用"),
		OutTradeNo:  core.String(tradeNo),
		NotifyUrl:   core.String(callbackBase + "/api/user/wechat/notify"),
		Amount:      &native.Amount{Total: core.Int64(int64(amountCents)), Currency: core.String("CNY")},
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "NATIVE_ORDER_FAIL", "message": "微信支付下单失败: " + err.Error()})
		return
	}
	codeUrl := codeUrlOf(resp)
	if codeUrl == "" {
		// 下单成功但没拿到 code_url:同样要关单,否则留脏单
		_, _ = svc.CloseOrder(c.Request.Context(), native.CloseOrderRequest{
			OutTradeNo: core.String(tradeNo), Mchid: core.String(operation_setting.WechatMchID),
		})
		c.JSON(http.StatusBadGateway, gin.H{"code": "NATIVE_ORDER_FAIL", "message": "微信支付下单返回空 code_url"})
		return
	}

	paymentCode, expiresAt, perr := service.X402Preorder(c.Request.Context(), codeUrlOf(resp))
	if perr != nil {
		// 预下单失败必须关单,否则微信侧留未支付脏订单
		_, _ = svc.CloseOrder(c.Request.Context(), native.CloseOrderRequest{
			OutTradeNo: core.String(tradeNo), Mchid: core.String(operation_setting.WechatMchID),
		})
		code := "PREORDER_FAIL"
		var xe *service.X402Error
		if errors.As(perr, &xe) {
			code = xe.Code
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": code, "message": perr.Error()})
		return
	}

	claim := common.GetRandomString(24)
	hold := &model.X402Hold{
		TradeNo:         tradeNo,
		AgentUserId:     agentUserId,
		ClaimToken:      claim,
		PaymentCodeHash: x402CodeHash(paymentCode),
		// Money(元,含分)是配额折算基准;Amount 对齐 TopUp.Amount 的整数元台账口径。
		// 单价必须配成整元(见 model/option.go X402AmountCents 校验),否则此处整除会少记台账。
		Money:     float64(amountCents) / 100,
		Amount:    int64(amountCents) / 100,
		Status:    model.X402HoldStatusPending,
		CreatedAt: common.GetTimestamp(),
		ExpiresAt: expiresAt,
	}
	if err := model.InsertX402Hold(hold); err != nil {
		// 挂账单没落地 → 后续无法履约也无法对账,必须关单再报错,别留脏单
		_, _ = svc.CloseOrder(c.Request.Context(), native.CloseOrderRequest{
			OutTradeNo: core.String(tradeNo), Mchid: core.String(operation_setting.WechatMchID),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"code": "HOLD_INSERT_FAILED", "message": err.Error()})
		return
	}

	body := gin.H{
		"code":         "PAYMENT_REQUIRED",
		"message":      fmt.Sprintf("本次%s需要支付 ¥%.2f", operation_setting.X402ServiceName, float64(amountCents)/100),
		"out_trade_no": tradeNo,
		"amount":       fmt.Sprintf("%.2f", float64(amountCents)/100),
		"currency":     "CNY",
		"WeixinPay": gin.H{
			"WeixinPay-Required": paymentCode,
			"prompt": "本次使用微信支付,请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay," +
				"以向用户申请支付授权。支付成功后必须同时携带 X-Out-Trade-No 与 WeixinPay-Required 两个请求头" +
				"重新请求本接口(只带订单号拿不到付费内容),否则无法履约。",
		},
	}
	c.Header("WeixinPay-Required", paymentCode)
	c.Header("X-Out-Trade-No", tradeNo)
	c.JSON(http.StatusPaymentRequired, body)
}

// skillRetry — ⑦⑧⑨:查单验证后履约。
//
// 已绑定 → 即时入账;未绑定 → 入 held 队列并在响应里回 claim 链接。
// 同一订单只履约一次(hold 状态机 + TopUp.trade_no 唯一约束双保险)。
func skillRetry(c *gin.Context, query string, agentUserId string, tradeNo string, paymentCode string) {
	if !strings.HasPrefix(tradeNo, x402TradeNoPrefix) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_OUT_TRADE_NO", "message": "订单号格式不符"})
		return
	}
	hold := model.GetX402HoldByTradeNo(tradeNo)
	if hold == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "ORDER_NOT_FOUND", "message": "订单不存在或已归档"})
		return
	}
	// 凭证校验:商户单号是可枚举串,只认单号 = 任何人猜到单号就能读走/认领别人的
	// 挂账。⑦步要求回显 WeixinPay-Required,与 ④ 步下发的哈希比对后才继续。
	if !x402CodeMatches(hold, paymentCode) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": "BAD_PAYMENT_CODE", "message": "WeixinPay-Required 与订单不匹配或缺失,请携支付码重试",
		})
		return
	}
	// ⑨ 幂等:此前已履约 → 直接回缓存结果,绝不重复入账
	if hold.Status == model.X402HoldStatusCredited {
		c.JSON(http.StatusOK, gin.H{
			"code": "SUCCESS", "message": "付费内容获取成功",
			"out_trade_no": tradeNo, "already_fulfilled": true,
			"content": x402Deliver(query, tradeNo, agentUserId),
		})
		return
	}
	svc := GetWechatClient()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "WECHAT_NOT_CONFIGURED", "message": "微信支付商户证书未配置,无法查单"})
		return
	}
	// 过期(hold 读时已被标 expired)不代表没扣款:payment_code 只活 15 分钟,而
	// Native code_url 更久,用户可能晚于 15 分钟才扫码。照常查单,查成功即复活挂账。
	txn, _, qerr := svc.QueryOrderByOutTradeNo(c.Request.Context(), native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(tradeNo), Mchid: core.String(operation_setting.WechatMchID),
	})
	if qerr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "QUERY_FAIL", "message": "查单失败: " + qerr.Error()})
		return
	}
	if txn == nil || txn.TradeState == nil || *txn.TradeState != "SUCCESS" {
		state := "UNKNOWN"
		if txn != nil && txn.TradeState != nil {
			state = *txn.TradeState
		}
		code, msg := "PAYMENT_NOT_COMPLETED", "支付未完成或处理中"
		if hold.Status == model.X402HoldStatusExpired {
			code, msg = "PAYMENT_EXPIRED", "支付码已过期,请重新发起调用(将生成新订单)"
		}
		c.JSON(http.StatusPaymentRequired, gin.H{"code": code, "message": msg, "trade_state": state})
		return
	}
	if hold.Status == model.X402HoldStatusExpired {
		common.SysLog("x402: " + tradeNo + " 已过期但查到扣款成功,复活挂账继续履约")
	}

	// 款已到账。身份判定优先用本次请求的 agent 身份,回落到 hold 上记录的 agent 身份。
	userId := resolveX402UserId(agentUserId, hold)
	if userId > 0 {
		// 异步 notify 可能已把 pending 推到 held;MarkX402Held 对 held 是 no-op。
		if err := model.MarkX402Held(hold); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "HOLD_MARK_FAILED", "message": err.Error()})
			return
		}
		credited, berr := model.BindX402Hold(hold, userId, c.ClientIP())
		if berr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "CREDIT_FAILED", "message": berr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": "SUCCESS", "message": "付费内容获取成功",
			"out_trade_no": tradeNo, "transaction_id": strOf(txn.TransactionId),
			"already_fulfilled": !credited, "content": x402Deliver(query, tradeNo, agentUserId),
		})
		return
	}

	// 未绑定 → 入队,钱不动,等 claim 绑定瞬间排空
	if err := model.MarkX402Held(hold); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "HOLD_MARK_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": "SUCCESS", "message": "付费内容获取成功(配额待账号认领后自动到账)",
		"out_trade_no": tradeNo, "transaction_id": strOf(txn.TransactionId),
		"already_fulfilled": false,
		"content": gin.H{
			"message":       "款项已收到。用微信打开下方链接完成登录/注册,配额即自动到账,无需重新支付。",
			"bind_url":      x402BindURL(c, hold.ClaimToken),
			"bind_deadline": hold.ExpiresAt,
			"quota_added":   0,
			"pending_bind":  true,
		},
	})
}

// SkillBindClaim — 认领入口(会话内)。前端在微信内先走现有 /api/oauth/wechat
// 自动登录/注册,再带 session 打这里。
func SkillBindClaim(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "请先登录", "success": false})
		return
	}
	claimX402Hold(c, strings.TrimSpace(c.Query("claim")), userId)
}

// SkillWeChatClaim — 免登录认领(服务号 / 小程序场景)。
//
// 用户在微信内打开 claim 链接时,页面带微信 OAuth code 直接打这里:后端用现有
// getWeChatIdByCode → resolveOrCreateWeChatUser 完成「识别 → 已注册即登录、
// 未注册即注册」,当场入账并写会话,省掉用户手动登录那一步。身份链路 100% 复用
// 现有扫码登录体系,不新增第二套注册逻辑。
func SkillWeChatClaim(c *gin.Context) {
	if !common.WeChatAuthEnabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "管理员未开启通过微信登录以及注册"})
		return
	}
	wechatId, err := getWeChatIdByCode(c.Query("code"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	user, uerr := resolveOrCreateWeChatUser(wechatId)
	if uerr != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": uerr.Error()})
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "用户已被封禁"})
		return
	}
	// 先落会话:即使认领环节失败,用户也是已登录态,可走会话内认领重试
	setupLogin(user, c)
	claimX402Hold(c, strings.TrimSpace(c.Query("claim")), user.Id)
}

// claimX402Hold — 认领公共流程:定位挂账单 → 必要时补查单 → 幂等入账 → 绑 agent 身份。
func claimX402Hold(c *gin.Context, claim string, userId int) {
	hold := model.ResolveX402HoldByClaim(claim)
	if hold == nil {
		c.JSON(http.StatusGone, gin.H{"message": "链接已过期或已被使用", "success": false})
		return
	}
	// Agent 可能没重试第⑦步(直接关掉),用户凭 claim 链接来认领时 hold 还是
	// pending;也可能是「payment_code 过期后才扫码付款」→ expired。两种情况都
	// 主动查单一次:微信确认扣款就补推 held,让钱能被认领入账(绝不在此处加款)。
	if hold.Status == model.X402HoldStatusPending || hold.Status == model.X402HoldStatusExpired {
		if x402OrderPaid(c.Request.Context(), hold.TradeNo) {
			if err := model.MarkX402Held(hold); err != nil {
				c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
				return
			}
		} else if hold.ExpiresAt > 0 && common.GetTimestamp() > hold.ExpiresAt {
			// 未扣款且已过 payment_code 有效期 → 钱没收到,拒认领
			c.JSON(http.StatusGone, gin.H{"success": false, "message": "支付未完成或已过期", "status": hold.Status})
			return
		}
	}
	credited, err := model.BindX402Hold(hold, userId, c.ClientIP())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if hold.Status != model.X402HoldStatusCredited && !credited {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "支付尚未完成,无法认领", "status": hold.Status})
		return
	}
	// 认领成功即完成 agent 身份 → 账号 绑定:此后该 agent 再付款直接即时入账,
	// 不再走挂账。绑定失败不影响已完成的入账,仅记录日志。
	if err := model.BindX402AgentId(hold.AgentUserId, userId); err != nil {
		common.SysError("x402 agent bind failed: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true, "credited": credited, "user_id": userId,
		"trade_no": hold.TradeNo, "money": hold.Money,
		"message": fmt.Sprintf("已绑定账户并到账 ¥%.2f 对应配额", hold.Money),
	})
}

// resolveX402UserId — agent 身份 → savvy user id。
//
// 查找顺序:① 本次请求带的 agentUserId ② hold 上记录的 agentUserId。
// 返回 0 表示未绑定,调用方入队等认领。
func resolveX402UserId(agentUserId string, hold *model.X402Hold) int {
	for _, id := range []string{agentUserId, hold.AgentUserId} {
		if id == "" {
			continue
		}
		if u := model.GetUserByX402AgentId(id); u != nil {
			return u.Id
		}
	}
	return 0
}

func x402BindURL(c *gin.Context, claim string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	if base == "" {
		base = schemeOf(c) + "://" + c.Request.Host
	}
	return base + "/api/x402/bind?claim=" + url.QueryEscape(claim)
}

func x402Deliver(query string, tradeNo string, agentUserId string) gin.H {
	// TODO(业务): 换成真实交付 —— 查询配额、调用 Hermes 网关、返回付费内容。
	return gin.H{"query_echo": query, "order": tradeNo, "delivered_at": common.GetTimestamp()}
}

func strOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// codeUrlOf — SDK 返回字段全是指针,集中兜 nil。
func codeUrlOf(resp *native.PrepayResponse) string {
	if resp == nil {
		return ""
	}
	return strOf(resp.CodeUrl)
}

// x402CodeHash / x402CodeMatches — ④步下发的 payment_code 只存哈希,⑦步回显后
// 定长比对。防止「猜到商户单号就能领取他人挂账」这一整类越权履约。
func x402CodeHash(paymentCode string) string {
	if paymentCode == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(paymentCode))
	return hex.EncodeToString(sum[:])
}

func x402CodeMatches(hold *model.X402Hold, paymentCode string) bool {
	if hold == nil || paymentCode == "" || hold.PaymentCodeHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(x402CodeHash(paymentCode)), []byte(hold.PaymentCodeHash)) == 1
}

// x402OrderPaid — 查单确认已扣款(claim 路径的补偿查询;⑧步主路径在 skillRetry)。
func x402OrderPaid(ctx context.Context, tradeNo string) bool {
	svc := GetWechatClient()
	if svc == nil {
		return false
	}
	txn, _, err := svc.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(tradeNo), Mchid: core.String(operation_setting.WechatMchID),
	})
	return err == nil && txn != nil && txn.TradeState != nil && *txn.TradeState == "SUCCESS"
}

// SkillHoldsStatus — 会话内查询:我(已绑定 agent 身份)名下有没有待认领的钱。
// 前端充值页/钱包页展示用。
func SkillHoldsStatus(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "请先登录"})
		return
	}
	list, err := model.ListX402HeldForUser(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	var total float64
	for _, h := range list {
		total += h.Money
		items = append(items, gin.H{
			"trade_no":   h.TradeNo,
			"money":      h.Money,
			"created_at": h.CreatedAt,
			"pay_time":   h.PayTime,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"pending_count": len(items), "pending_money": total, "items": items},
	})
}

func schemeOf(c *gin.Context) string {
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}
