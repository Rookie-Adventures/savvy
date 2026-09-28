package controller

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

// SkillPay（微信 Agent Pay X402）付费技能入口，对齐官方 9 步协议。
// 两个动作（kind）：
//   - qa    付费问答：首次 402（PRICE_FEN 定价）→ 付款重试 → 履约=专用 token 走 relay 一次 AI 问答
//   - topup 额度充值（服务包）：金额由智能体传（amount_yuan，0.01~5000 元）→ 付款重试 →
//     实付为准 → TopUp(wechat_skillpay) + claim_token → 用户登录 Savvy 认领入账
//     （对齐 alipay_agent 模式：申报值下单、实付为准、claim_token、蚂蚁链存证）
type SkillInvokeRequest struct {
	Action     string  `json:"action"`      // ""/"qa" = 付费问答；"topup" = 额度充值
	Query      string  `json:"query"`       // qa 必填；topup 忽略
	AmountYuan float64 `json:"amount_yuan"` // topup 必填：充值金额（元）
}

// generateSkillPayOutTradeNo WX402_(6) + 14 位时间戳 + 12 位随机 = 32 位（微信上限）。
func generateSkillPayOutTradeNo() string {
	return fmt.Sprintf("WX402_%s%s", time.Now().Format("20060102150405"), common.GetRandomString(12))
}

// 首请求限流：/api/skill/invoke 匿名可达，每次"首请求"都会真的向微信下一单 + 写一行订单。
// 2026-09-28 冒烟实测同一 IP 连打 30 次全部 402（零拦截），所以必须自己拦：
// 只限"建单"，不限带 X-Out-Trade-No 的重试（重试是付款后的正常动作，卡它会挡住真实买家）。
// 阈值取宽松值：服务号 webview 与外部 Agent 都算得上"一次充值 ≤ 2 次建单"，正常用户碰不到。
var (
	skillPayCreateHourLimiter common.InMemoryRateLimiter
	skillPayCreateDayLimiter  common.InMemoryRateLimiter
)

func init() {
	skillPayCreateHourLimiter.Init(time.Hour)
	skillPayCreateDayLimiter.Init(24 * time.Hour)
}

func allowSkillPayOrderCreate(ip string) bool {
	return skillPayCreateHourLimiter.Request("skh:"+ip, 30, 3600) &&
		skillPayCreateDayLimiter.Request("skd:"+ip, 100, 86400)
}

func respondSkillPayCreateLimited(c *gin.Context) {
	c.JSON(http.StatusTooManyRequests, gin.H{
		"code": "TOO_MANY_ORDERS", "message": "下单过于频繁，请稍后再试",
	})
}

// respondSkillPay402 统一 402 响应：Header（支付码+订单号）+ Body（WeixinPay 提示块，兼容只读 body 的 Agent）。
// claimURL 非空时一并给出：充值单的认领凭据在**建单时**就预生成，链接先交到用户手上，
// 这样"付完款但 Agent 换了会话/丢了 payment_code"不再等于钱没人能领（详见 SkillPayOrder.ClaimToken）。
func respondSkillPay402(c *gin.Context, paymentCode, outTradeNo, amountYuan, title, claimURL string) {
	c.Header("WeixinPay-Required", paymentCode)
	c.Header("X-Out-Trade-No", outTradeNo)
	body := gin.H{
		"code":    "PAYMENT_REQUIRED",
		"message": fmt.Sprintf("%s需要支付 ¥%s", title, amountYuan),
		"WeixinPay": gin.H{
			"WeixinPay-Required": paymentCode,
			"prompt":             "本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。",
		},
		"out_trade_no": outTradeNo,
		"amount":       amountYuan,
		"currency":     "CNY",
	}
	if claimURL != "" {
		body["claim_url"] = claimURL
		body["claim_hint"] = "请原样把 claim_url 转述给用户：支付成功后该链接即可登录/注册认领入账，即使本次会话中断也不会丢失这笔钱。"
	}
	c.JSON(http.StatusPaymentRequired, body)
}

// verifySkillPayCode 用 SHA-256 + 常量时间比对校验付款码，避免把"知道订单号"当成"付了钱"。
// 两侧都先做 SHA-256 再常量时间比对：长度归一、且不会按字节逐个短路泄漏差异位置。
func verifySkillPayCode(stored, presented string) bool {
	if stored == "" || presented == "" {
		return false
	}
	a := sha256.Sum256([]byte(stored))
	b := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// SkillInvoke POST /api/skill/invoke（公开路由 + TryUserAuth：服务号 webview 有登录态，外部 Agent 无）。
func SkillInvoke(c *gin.Context) {
	if !operation_setting.IsSkillPayConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SKILLPAY_DISABLED", "message": "SkillPay 未启用或配置不全"})
		return
	}
	var req SkillInvokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "无效 JSON"})
		return
	}
	outTradeNo := c.GetHeader("X-Out-Trade-No")
	if outTradeNo != "" {
		handleSkillPayRetry(c, req, outTradeNo)
		return
	}
	if req.Action == "topup" {
		handleSkillPayTopUpFirstRequest(c, req.AmountYuan)
		return
	}
	if req.Query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "缺少 query"})
		return
	}
	// ponytail: 只有 AI 问答这条要服务端代跑一次模型,缺 relay 就单独拒;不能因此挡住充值
	if !operation_setting.IsSkillPayRelayConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SKILLPAY_RELAY_DISABLED", "message": "AI 问答履约未配置(SKILLPAY_RELAY_*)"})
		return
	}
	handleSkillPayQAFirstRequest(c)
}

// createSkillPayNativeOrder Step 2：Native 下单（金额/描述按动作），返回 code_url。
func createSkillPayNativeOrder(c *gin.Context, outTradeNo string, totalFen int64, description string) (string, error) {
	svc := GetWechatClient()
	if svc == nil {
		return "", fmt.Errorf("微信支付未配置")
	}
	resp, _, err := svc.Prepay(context.Background(), native.PrepayRequest{
		Appid:       core.String(operation_setting.WechatAppId),
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String(description),
		OutTradeNo:  core.String(outTradeNo),
		NotifyUrl:   core.String(service.GetCallbackAddress() + "/api/skill/notify"),
		Amount: &native.Amount{
			Total:    core.Int64(totalFen),
			Currency: core.String("CNY"),
		},
	})
	if err != nil {
		return "", err
	}
	return *resp.CodeUrl, nil
}

// closeSkillPayNativeOrder 预下单失败时关微信单，避免脏订单。
func closeSkillPayNativeOrder(c *gin.Context, outTradeNo string) {
	svc := GetWechatClient()
	if svc == nil {
		return
	}
	if _, err := svc.CloseOrder(context.Background(), native.CloseOrderRequest{
		OutTradeNo: core.String(outTradeNo),
		Mchid:      core.String(operation_setting.WechatMchID),
	}); err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay close order failed: out_trade_no=%s err=%v", outTradeNo, err))
	}
}

// handleSkillPayQAFirstRequest 场景 qa-一：付费问答首请求 → 402。
func handleSkillPayQAFirstRequest(c *gin.Context) {
	if !allowSkillPayOrderCreate(c.ClientIP()) {
		respondSkillPayCreateLimited(c)
		return
	}
	outTradeNo := generateSkillPayOutTradeNo()
	codeUrl, err := createSkillPayNativeOrder(c, outTradeNo, int64(operation_setting.SkillPayPriceFen), "Savvy AI 问答")
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay prepay failed: out_trade_no=%s err=%v", outTradeNo, err))
		_ = model.MarkSkillPayOrderClosed(outTradeNo)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ORDER_FAIL", "message": "下单失败"})
		return
	}
	paymentCode, err := service.SkillPayX402Preorder("code_url", codeUrl)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay preorder failed: out_trade_no=%s err=%v", outTradeNo, err))
		closeSkillPayNativeOrder(c, outTradeNo)
		_ = model.MarkSkillPayOrderClosed(outTradeNo)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "PREORDER_FAIL", "message": "预下单失败"})
		return
	}
	order := &model.SkillPayOrder{
		OutTradeNo:  outTradeNo,
		PaymentCode: paymentCode,
		Kind:        model.SkillPayKindQA,
		MoneyYuan:   float64(operation_setting.SkillPayPriceFen) / 100,
		Status:      model.SkillPayStatusPending,
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay insert order failed: out_trade_no=%s err=%v", outTradeNo, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_FAIL", "message": "订单落库失败"})
		return
	}
	respondSkillPay402(c, paymentCode, outTradeNo, fmt.Sprintf("%.2f", float64(operation_setting.SkillPayPriceFen)/100), "本次 AI 问答", "")
}

// handleSkillPayTopUpFirstRequest 场景 topup-一：服务包充值首请求 → 402（金额智能体申报）。
func handleSkillPayTopUpFirstRequest(c *gin.Context, amountYuan float64) {
	totalFen, ok := agentTopUpAmountCents(amountYuan)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "AMOUNT_OUT_OF_RANGE", "message": "充值金额需在 0.01~5000 元之间"})
		return
	}
	if !allowSkillPayOrderCreate(c.ClientIP()) {
		respondSkillPayCreateLimited(c)
		return
	}
	// 认领凭据在建单时就生成：payment_code 只有 15 分钟且属于当次会话，
	// 等履约才发等于把"钱能不能被领回"押在 Agent 不掉线上。
	claimToken, err := newClaimToken()
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay topup gen claim token failed: err=%v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_FAIL", "message": "下单失败"})
		return
	}
	outTradeNo := generateSkillPayOutTradeNo()
	codeUrl, err := createSkillPayNativeOrder(c, outTradeNo, totalFen, "栗橙科技-服务包")
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay topup prepay failed: out_trade_no=%s err=%v", outTradeNo, err))
		_ = model.MarkSkillPayOrderClosed(outTradeNo)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ORDER_FAIL", "message": "下单失败"})
		return
	}
	paymentCode, err := service.SkillPayX402Preorder("code_url", codeUrl)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay topup preorder failed: out_trade_no=%s err=%v", outTradeNo, err))
		closeSkillPayNativeOrder(c, outTradeNo)
		_ = model.MarkSkillPayOrderClosed(outTradeNo)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "PREORDER_FAIL", "message": "预下单失败"})
		return
	}
	order := &model.SkillPayOrder{
		OutTradeNo:  outTradeNo,
		PaymentCode: paymentCode,
		Kind:        model.SkillPayKindTopUp,
		// 申报值只用于下单口径；金额一律以"分成"后的实收为准回写，
		// 否则 0.015 这类输入会出现"提示 ¥0.01、实际扣 ¥0.02"的口径分裂（2026-09-28 实测）
		MoneyYuan:  float64(totalFen) / 100,
		Status:     model.SkillPayStatusPending,
		ClaimToken: claimToken,
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay insert topup order failed: out_trade_no=%s err=%v", outTradeNo, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_FAIL", "message": "订单落库失败"})
		return
	}
	claimURL := buildAgentClaimUrl(system_setting.ServerAddress, claimToken, outTradeNo)
	respondSkillPay402(c, paymentCode, outTradeNo, fmt.Sprintf("%.2f", float64(totalFen)/100), "Savvy 额度充值", claimURL)
}

// handleSkillPayRetry 场景二：支付后重试 → 查单 → 按 kind 履约（幂等）。
func handleSkillPayRetry(c *gin.Context, req SkillInvokeRequest, outTradeNo string) {
	order := model.GetSkillPayOrderByTradeNo(outTradeNo)
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "ORDER_NOT_FOUND", "message": "订单不存在"})
		return
	}
	// ponytail: 付款码校验必须在幂等缓存之前——否则拿到订单号的人能直接取走已履约内容。
	// out_trade_no 尾部虽有随机串，但订单号会出现在日志/代理链路里，不能当作凭证本身。
	if !verifySkillPayCode(order.PaymentCode, c.GetHeader("WeixinPay-Required")) {
		logger.LogError(c, fmt.Sprintf("skillpay retry with invalid payment code: out_trade_no=%s", outTradeNo))
		c.JSON(http.StatusUnauthorized, gin.H{"code": "PAYMENT_CODE_INVALID", "message": "缺少或错误的 WeixinPay-Required"})
		return
	}
	LockOrder(outTradeNo)
	defer UnlockOrder(outTradeNo)

	// 幂等：已履约直接回缓存
	if content, ok := model.GetSkillPayOrderFulfilledContent(outTradeNo); ok {
		c.JSON(http.StatusOK, gin.H{
			"code": "SUCCESS", "message": "付费内容获取成功",
			"out_trade_no": outTradeNo, "content": content, "already_fulfilled": true,
		})
		return
	}

	tx, err := wechatQueryOrderFn(outTradeNo)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay query order failed: out_trade_no=%s err=%v", outTradeNo, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "QUERY_FAIL", "message": "查单失败"})
		return
	}
	transactionId := ""
	if tx.TransactionId != nil {
		transactionId = *tx.TransactionId
	}
	if tx.TradeState == nil || *tx.TradeState != "SUCCESS" {
		state := "UNKNOWN"
		if tx.TradeState != nil {
			state = *tx.TradeState
		}
		respondSkillPayNotPaid(c, outTradeNo, state)
		return
	}
	// 付款人身份(下单 AppID 空间的 openid):查单是主来源,notify 补记兜底
	payerOpenid := ""
	if tx.Payer != nil && tx.Payer.Openid != nil {
		payerOpenid = *tx.Payer.Openid
	}
	_ = model.MarkSkillPayOrderPaid(outTradeNo, transactionId, payerOpenid)
	// notify 先于本行把单标成 paid 时 Mark 的条件更新会落空,这里补记(仅空值写入,幂等)
	_ = model.BindSkillPayPayerOpenid(outTradeNo, payerOpenid)

	// 实付金额（分→元）为入账依据，Agent 申报值不作数
	paidFen := int64(0)
	if tx.Amount != nil && tx.Amount.Total != nil {
		paidFen = *tx.Amount.Total
	}
	paidYuan := float64(paidFen) / 100

	var content string
	if order.Kind == model.SkillPayKindTopUp {
		content, err = fulfillSkillPayTopUp(c.Request.Context(), c.ClientIP(), c.GetInt("id"),
			outTradeNo, transactionId, paidYuan, payerOpenid, order.ClaimToken)
	} else {
		content, err = service.SkillPayFulfill(req.Query)
	}
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay fulfill failed: out_trade_no=%s kind=%s err=%v", outTradeNo, order.Kind, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "FULFILL_FAIL", "message": "履约失败，请重试"})
		return
	}

	rows, err := model.FulfillSkillPayOrder(outTradeNo, transactionId, content)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay fulfill save failed: out_trade_no=%s err=%v", outTradeNo, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_FAIL", "message": "履约落库失败"})
		return
	}
	alreadyFulfilled := rows == 0
	if alreadyFulfilled {
		content, _ = model.GetSkillPayOrderFulfilledContent(outTradeNo)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": "SUCCESS", "message": "付费内容获取成功",
		"out_trade_no": outTradeNo, "transaction_id": transactionId,
		"content": content, "already_fulfilled": alreadyFulfilled,
	})
}

// fulfillSkillPayTopUp 充值履约：实付金额建 TopUp(wechat_skillpay)，复用建单时预生成的 claim_token。
// 身份判定三级：① 请求带登录态（服务号 webview）→ 直接入账；② payer openid 命中
// users.wechat_id（扫码登录体系同源，Native AppID 空间）→ 老客户零点击直入账；
// ③ 都不命中 → 发 claim_token，登录/注册后认领入账（不自动注册，钱必须有人认领）。
// sessionUserId 由调用方给（HTTP=登录态，后台兜底=0），故本函数不依赖 gin，可被 sweep 复用。
func fulfillSkillPayTopUp(ctx context.Context, clientIP string, sessionUserId int, outTradeNo, transactionId string, paidYuan float64, payerOpenid, claimToken string) (string, error) {
	if claimToken == "" {
		// 旧单（本改动之前建的）没有预生成凭据，临时补一个，行为与从前一致
		tok, err := newClaimToken()
		if err != nil {
			return "", fmt.Errorf("gen claim token: %w", err)
		}
		claimToken = tok
		_ = model.SetSkillPayOrderClaimToken(outTradeNo, claimToken)
	}
	userId := sessionUserId
	creditedVia := "" // 直入账来源，供日志与回推决策
	if userId <= 0 && payerOpenid != "" {
		if uid, ok := model.GetUserIdByWeChatNativeOpenid(payerOpenid); ok {
			userId = uid
			creditedVia = "payer_openid"
		}
	}
	// 额度换算预检(在 TopUp 落库前)：金额过小换算为 0 时不硬失败——
	// 已入账路径报错会让 AI 无限重试(单号唯一索引卡履约)，降级为游客发认领。
	amount := int64(0)
	if userId > 0 {
		group, gerr := model.GetUserGroup(userId, true)
		if gerr != nil {
			return "", fmt.Errorf("get user group: %w", gerr)
		}
		amount = agentQuotaAmountFromMoney(paidYuan, group)
		if amount <= 0 {
			logger.LogWarn(ctx, fmt.Sprintf("skillpay topup amount too small, fallback to claim: out_trade_no=%s user=%d money=%.2f", outTradeNo, userId, paidYuan))
			userId = 0
			creditedVia = ""
		}
	}
	topUp := &model.TopUp{
		UserId:          userId,
		TradeNo:         outTradeNo,
		ClaimToken:      claimToken,
		Money:           paidYuan,
		PaymentMethod:   model.PaymentMethodWechat,
		PaymentProvider: model.PaymentProviderWechatSkillPay,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess, // 查单已确认 SUCCESS，直接终态
	}
	if err := topUp.Insert(); err != nil {
		// 唯一索引 out_trade_no 冲突 = 并发已建，静默幂等
		return "", fmt.Errorf("insert topup: %w", err)
	}

	claimURL := buildAgentClaimUrl(system_setting.ServerAddress, claimToken, outTradeNo)
	if userId > 0 {
		quotaToAdd := int(decimal.NewFromInt(amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if err := model.IncreaseUserQuota(userId, quotaToAdd, true); err != nil {
			return "", fmt.Errorf("increase quota: %w", err)
		}
		via := "登录态"
		if creditedVia == "payer_openid" {
			via = "付款人openid匹配"
		}
		model.RecordTopupLog(userId,
			fmt.Sprintf("使用微信智能体充值成功(%s)，充值金额: %v，支付金额：%f", via, logger.LogQuota(quotaToAdd), paidYuan),
			clientIP, model.PaymentMethodWechat, model.PaymentProviderWechatSkillPay)
		return fmt.Sprintf("充值成功，到账额度: %v", logger.LogQuota(quotaToAdd)), nil
	}
	// 游客：凭据交付，登录/注册后认领入账
	return fmt.Sprintf("充值成功。认领凭据 claim_token=%s，请打开 %s 登录或注册后自动入账（凭据已随本消息交付，请妥善保存）", claimToken, claimURL), nil
}

func respondSkillPayNotPaid(c *gin.Context, outTradeNo, tradeState string) {
	c.Header("X-Out-Trade-No", outTradeNo)
	c.JSON(http.StatusPaymentRequired, gin.H{
		"code":         "PAYMENT_NOT_COMPLETED",
		"message":      fmt.Sprintf("支付未完成，当前状态: %s", tradeState),
		"out_trade_no": outTradeNo,
		"trade_state":  tradeState,
	})
}

// SkillPayNotify POST /api/skill/notify：微信异步回调，仅标记已支付（履约统一在重试时查单触发）。
func SkillPayNotify(c *gin.Context) {
	if GetWechatClient() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "FAIL", "message": "unconfigured"})
		return
	}
	finalize := func(c *gin.Context, tradeNo, payload string) error {
		order := model.GetSkillPayOrderByTradeNo(tradeNo)
		if order == nil {
			return fmt.Errorf("order not found")
		}
		if order.Fulfilled || order.Status == model.SkillPayStatusPaid {
			// 已 paid 仍可能缺身份(查单先到但响应无 payer)：补记一次，仅空值写入
			var early struct {
				Payer struct {
					Openid string `json:"openid"`
				} `json:"payer"`
			}
			if common.Unmarshal([]byte(payload), &early) == nil {
				_ = model.BindSkillPayPayerOpenid(tradeNo, early.Payer.Openid)
			}
			return nil // 幂等
		}
		var detail struct {
			TransactionId string `json:"transaction_id"`
			Payer         struct {
				Openid string `json:"openid"`
			} `json:"payer"`
		}
		_ = common.Unmarshal([]byte(payload), &detail)
		return model.MarkSkillPayOrderPaid(tradeNo, detail.TransactionId, detail.Payer.Openid)
	}
	handleWxNotify(c, finalize)
}
