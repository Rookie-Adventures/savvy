package controller

import (
	"context"
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
//   - topup 额度充值（服务包）：金额由智能体传（amount_yuan，1~5000 元）→ 付款重试 →
//     实付为准 → TopUp(wechat_skillpay) + claim_token → 用户登录 Savvy 认领入账
//     （对齐 alipay_agent 模式：申报值下单、实付为准、claim_token、蚂蚁链存证）
type SkillInvokeRequest struct {
	Action     string  `json:"action"`                // ""/"qa" = 付费问答；"topup" = 额度充值
	Query      string  `json:"query"`                 // qa 必填；topup 忽略
	AmountYuan float64 `json:"amount_yuan"`           // topup 必填：充值金额（元）
}

// generateSkillPayOutTradeNo WX402_(6) + 14 位时间戳 + 12 位随机 = 32 位（微信上限）。
func generateSkillPayOutTradeNo() string {
	return fmt.Sprintf("WX402_%s%s", time.Now().Format("20060102150405"), common.GetRandomString(12))
}

// respondSkillPay402 统一 402 响应：Header（支付码+订单号）+ Body（WeixinPay 提示块，兼容只读 body 的 Agent）。
func respondSkillPay402(c *gin.Context, paymentCode, outTradeNo, amountYuan, title string) {
	c.Header("WeixinPay-Required", paymentCode)
	c.Header("X-Out-Trade-No", outTradeNo)
	c.JSON(http.StatusPaymentRequired, gin.H{
		"code":    "PAYMENT_REQUIRED",
		"message": fmt.Sprintf("%s需要支付 ¥%s", title, amountYuan),
		"WeixinPay": gin.H{
			"WeixinPay-Required": paymentCode,
			"prompt":             "本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。",
		},
		"out_trade_no": outTradeNo,
		"amount":       amountYuan,
		"currency":     "CNY",
	})
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
	respondSkillPay402(c, paymentCode, outTradeNo, fmt.Sprintf("%.2f", float64(operation_setting.SkillPayPriceFen)/100), "本次 AI 问答")
}

// handleSkillPayTopUpFirstRequest 场景 topup-一：服务包充值首请求 → 402（金额智能体申报）。
func handleSkillPayTopUpFirstRequest(c *gin.Context, amountYuan float64) {
	if amountYuan < 1 || amountYuan > 5000 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "AMOUNT_OUT_OF_RANGE", "message": "充值金额需在 1~5000 元之间"})
		return
	}
	outTradeNo := generateSkillPayOutTradeNo()
	totalFen := int64(amountYuan * 100)
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
		MoneyYuan:   amountYuan,
		Status:      model.SkillPayStatusPending,
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay insert topup order failed: out_trade_no=%s err=%v", outTradeNo, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_FAIL", "message": "订单落库失败"})
		return
	}
	respondSkillPay402(c, paymentCode, outTradeNo, fmt.Sprintf("%.2f", amountYuan), "Savvy 额度充值")
}

// handleSkillPayRetry 场景二：支付后重试 → 查单 → 按 kind 履约（幂等）。
func handleSkillPayRetry(c *gin.Context, req SkillInvokeRequest, outTradeNo string) {
	order := model.GetSkillPayOrderByTradeNo(outTradeNo)
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "ORDER_NOT_FOUND", "message": "订单不存在"})
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
	_ = model.MarkSkillPayOrderPaid(outTradeNo, transactionId)

	// 实付金额（分→元）为入账依据，Agent 申报值不作数
	paidFen := int64(0)
	if tx.Amount != nil && tx.Amount.Total != nil {
		paidFen = *tx.Amount.Total
	}
	paidYuan := float64(paidFen) / 100

	var content string
	if order.Kind == model.SkillPayKindTopUp {
		content, err = fulfillSkillPayTopUp(c, outTradeNo, transactionId, paidYuan)
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

// fulfillSkillPayTopUp 充值履约：实付金额建 TopUp(wechat_skillpay)。
// 有登录态（服务号 webview）：直接入账；游客：发 claim_token，登录后认领入账。
func fulfillSkillPayTopUp(c *gin.Context, outTradeNo, transactionId string, paidYuan float64) (string, error) {
	userId := c.GetInt("id") // TryUserAuth：游客为 0
	claimToken, err := newClaimToken()
	if err != nil {
		return "", fmt.Errorf("gen claim token: %w", err)
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

	claimURL := system_setting.ServerAddress + "/agent"
	if userId > 0 {
		group, gerr := model.GetUserGroup(userId, true)
		if gerr != nil {
			return "", fmt.Errorf("get user group: %w", gerr)
		}
		amount := agentQuotaAmountFromMoney(paidYuan, group)
		if amount <= 0 {
			return "", fmt.Errorf("金额过小，换算额度为 0")
		}
		quotaToAdd := int(decimal.NewFromInt(amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if err := model.IncreaseUserQuota(userId, quotaToAdd, true); err != nil {
			return "", fmt.Errorf("increase quota: %w", err)
		}
		model.RecordTopupLog(userId,
			fmt.Sprintf("使用微信智能体充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), paidYuan),
			c.ClientIP(), model.PaymentMethodWechat, model.PaymentProviderWechatSkillPay)
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
			return nil // 幂等
		}
		var detail struct {
			TransactionId string `json:"transaction_id"`
		}
		_ = common.Unmarshal([]byte(payload), &detail)
		return model.MarkSkillPayOrderPaid(tradeNo, detail.TransactionId)
	}
	handleWxNotify(c, finalize)
}
