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

	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

// SkillPay（微信 Agent Pay X402）付费技能入口，对齐官方 9 步协议：
// 首次请求(无 X-Out-Trade-No) → Native 下单 → AI 预下单 → 402 + WeixinPay-Required
// 支付后重试(携 X-Out-Trade-No) → 查单 SUCCESS → 履约(一次 AI 问答) → 200 + content。
// 幂等：同单只履约一次（DB 原子翻转 fulfilled），重复重试返回缓存。
type SkillInvokeRequest struct {
	Query string `json:"query"`
}

// respondSkillPay402 统一 402 响应：Header（支付码+订单号）+ Body（WeixinPay 提示块，兼容只读 body 的 Agent）。
func respondSkillPay402(c *gin.Context, paymentCode, outTradeNo, amountYuan string) {
	c.Header("WeixinPay-Required", paymentCode)
	c.Header("X-Out-Trade-No", outTradeNo)
	c.JSON(http.StatusPaymentRequired, gin.H{
		"code":    "PAYMENT_REQUIRED",
		"message": fmt.Sprintf("本次 AI 问答需要支付 ¥%s", amountYuan),
		"WeixinPay": gin.H{
			"WeixinPay-Required": paymentCode,
			"prompt":             "本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。",
		},
		"out_trade_no": outTradeNo,
		"amount":       amountYuan,
		"currency":     "CNY",
	})
}

// SkillInvoke POST /api/skill/invoke（公开路由，Agent 无登录态）。
func SkillInvoke(c *gin.Context) {
	if !operation_setting.IsSkillPayConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SKILLPAY_DISABLED", "message": "SkillPay 未启用或配置不全"})
		return
	}
	var req SkillInvokeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "缺少 query"})
		return
	}
	if outTradeNo := c.GetHeader("X-Out-Trade-No"); outTradeNo != "" {
		handleSkillPayRetry(c, req.Query, outTradeNo)
		return
	}
	handleSkillPayFirstRequest(c)
}

// handleSkillPayFirstRequest 场景一：Native 下单 → X402 预下单 → 402。
func handleSkillPayFirstRequest(c *gin.Context) {
	svc := GetWechatClient()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "UNCONFIGURED", "message": "微信支付未配置"})
		return
	}
	// WX402_(6) + 14 位时间戳 + 12 位随机 = 32 位（微信上限）
	outTradeNo := fmt.Sprintf("WX402_%s%s", time.Now().Format("20060102150405"), common.GetRandomString(12))
	order := &model.SkillPayOrder{
		OutTradeNo: outTradeNo,
		Status:     model.SkillPayStatusPending,
	}

	resp, _, err := svc.Prepay(context.Background(), native.PrepayRequest{
		Appid:       core.String(operation_setting.WechatAppId),
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String("Savvy AI 问答"),
		OutTradeNo:  core.String(outTradeNo),
		NotifyUrl:   core.String(service.GetCallbackAddress() + "/api/skill/notify"),
		Amount: &native.Amount{
			Total:    core.Int64(int64(operation_setting.SkillPayPriceFen)),
			Currency: core.String("CNY"),
		},
	})
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay prepay failed: out_trade_no=%s err=%v", outTradeNo, err))
		_ = model.MarkSkillPayOrderClosed(outTradeNo)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ORDER_FAIL", "message": "下单失败"})
		return
	}

	paymentCode, err := service.SkillPayX402Preorder("code_url", *resp.CodeUrl)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay preorder failed: out_trade_no=%s err=%v", outTradeNo, err))
		// 预下单失败关微信单，避免脏订单；再关本地单
		if _, cerr := svc.CloseOrder(context.Background(), native.CloseOrderRequest{
			OutTradeNo: core.String(outTradeNo),
			Mchid:      core.String(operation_setting.WechatMchID),
		}); cerr != nil {
			logger.LogError(c, fmt.Sprintf("skillpay close order failed: out_trade_no=%s err=%v", outTradeNo, cerr))
		}
		_ = model.MarkSkillPayOrderClosed(outTradeNo)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "PREORDER_FAIL", "message": "预下单失败"})
		return
	}

	order.PaymentCode = paymentCode
	if err := order.Insert(); err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay insert order failed: out_trade_no=%s err=%v", outTradeNo, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_FAIL", "message": "订单落库失败"})
		return
	}
	amountYuan := fmt.Sprintf("%.2f", float64(operation_setting.SkillPayPriceFen)/100)
	respondSkillPay402(c, paymentCode, outTradeNo, amountYuan)
}

// handleSkillPayRetry 场景二：支付后重试 → 查单 → 幂等履约。
func handleSkillPayRetry(c *gin.Context, query, outTradeNo string) {
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
			"out_trade_no": outTradeNo, "transaction_id": order.TransactionId,
			"content": content, "already_fulfilled": true,
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

	content, err := service.SkillPayFulfill(query)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay fulfill failed: out_trade_no=%s err=%v", outTradeNo, err))
		// 已付款未履约：500 让 Agent 重试（幂等保护不会重复扣内容费）
		c.JSON(http.StatusInternalServerError, gin.H{"code": "FULFILL_FAIL", "message": "履约失败，请重试"})
		return
	}
	rows, err := model.FulfillSkillPayOrder(outTradeNo, transactionId, content)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay fulfill save failed: out_trade_no=%s err=%v", outTradeNo, err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_FAIL", "message": "履约落库失败"})
		return
	}
	alreadyFulfilled := rows == 0 // 并发下别的请求先履约了，回读缓存保证一致性
	if alreadyFulfilled {
		content, _ = model.GetSkillPayOrderFulfilledContent(outTradeNo)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": "SUCCESS", "message": "付费内容获取成功",
		"out_trade_no": outTradeNo, "transaction_id": transactionId,
		"content": content, "already_fulfilled": alreadyFulfilled,
	})
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
