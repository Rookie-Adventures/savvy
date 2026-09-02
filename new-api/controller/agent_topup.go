package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
)

// agentQuotaAmountFromMoney 是 getPayMoney 的逆运算: 实付 RMB → 额度单位数量。
// ponytail: 不套 AmountDiscount(那是预设金额档位的促销,智能体订单金额任意,无档位可配)。
// 返回单位与 TopUp.Amount 语义一致: 非 TOKENS 展示 = USD 整数; TOKENS 展示 = token 数,
// 保证认领/入账处 quotaToAdd = Amount * QuotaPerUnit 与现有 AlipayNotify 同构。
func agentQuotaAmountFromMoney(money float64, group string) int64 {
	dMoney := decimal.NewFromFloat(money)
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	// Price 被误配为 0 时 decimal.Div 会 panic,与 ratio 同款兜底
	price := operation_setting.Price
	if price == 0 {
		price = 1
	}
	usd := dMoney.Div(decimal.NewFromFloat(price)).
		Div(decimal.NewFromFloat(ratio))
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return usd.Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	return usd.Round(0).IntPart()
}

// newClaimToken 生成 128-bit 随机认领凭据(hex 32 字符)。
// 不用 common.GetRandomString: 认领凭据是钱的钥匙,必须 crypto/rand。
func newClaimToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// 游客聊天 IP 限额。复用 common.InMemoryRateLimiter(滑动窗口+定时淘汰),hour/day 各一实例;
// 匿名公网端点,自研 map 无淘汰会无界增长(review I-1)。
var (
	guestChatHourLimiter common.InMemoryRateLimiter
	guestChatDayLimiter  common.InMemoryRateLimiter
)

func init() {
	resetGuestChatLimiter()
}

func resetGuestChatLimiter() {
	guestChatHourLimiter = common.InMemoryRateLimiter{}
	guestChatHourLimiter.Init(time.Hour)
	guestChatDayLimiter = common.InMemoryRateLimiter{}
	guestChatDayLimiter.Init(24 * time.Hour)
}

func allowGuestChat(ip string) bool {
	hourLimit, dayLimit := operation_setting.AgentGuestChatHourLimit, operation_setting.AgentGuestChatDayLimit
	if hourLimit <= 0 {
		hourLimit = 10
	}
	if dayLimit <= 0 {
		dayLimit = 50
	}
	if !guestChatHourLimiter.Request(ip, hourLimit, 3600) {
		return false
	}
	return guestChatDayLimiter.Request(ip, dayLimit, 86400)
}

// RegisterAgentTopUp 游客/用户下单后登记 MCP 订单,发放认领凭据。
// 金额此时只是"申报值"(来自支付链接),入账以 completeAgentTopUp 的支付宝侧金额为准。
func RegisterAgentTopUp(c *gin.Context) {
	var req struct {
		OutTradeNo string `json:"out_trade_no"`
	}
	if err := c.ShouldBindJSON(&req); err != nil ||
		strings.TrimSpace(req.OutTradeNo) == "" || len(req.OutTradeNo) > 64 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	outTradeNo := strings.TrimSpace(req.OutTradeNo)
	if model.GetTopUpByTradeNo(outTradeNo) != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单已登记"})
		return
	}
	token, err := newClaimToken()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "登记失败"})
		return
	}
	topUp := &model.TopUp{
		UserId:          c.GetInt("id"), // 游客为 0,认领时再绑
		TradeNo:         outTradeNo,
		ClaimToken:      token,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipayAgent,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "登记失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"claim_token": token}})
}

// AgentTopUpStatus 凭 claim_token 查订单状态;pending 超过 10 秒时顺手向支付宝查单
// (兜底通道: 百炼 MCP 若配不了 AP_NOTIFY_URL,到账感知全靠这里)。token 即凭据,无需登录。
func AgentTopUpStatus(c *gin.Context) {
	token := strings.TrimSpace(c.Query("claim_token"))
	if len(token) != 32 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	topUp := model.GetTopUpByClaimToken(token)
	if topUp == nil || topUp.PaymentProvider != model.PaymentProviderAlipayAgent {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}
	if topUp.Status == common.TopUpStatusPending && time.Now().Unix()-topUp.CreateTime > 10 {
		tryCompleteAgentTopUpByQuery(topUp, c.ClientIP())
		topUp = model.GetTopUpByClaimToken(token)
		if topUp == nil {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"status":  topUp.Status,
		"money":   topUp.Money,
		"claimed": topUp.UserId != 0,
	}})
}

// tryCompleteAgentTopUpByQuery 用站点现有支付宝 client 按 out_trade_no 查单。
// 前提(验证项 V0): MCP 订单与站点支付配置同 APPID。查不到/出错静默返回,下次轮询再试。
func tryCompleteAgentTopUpByQuery(topUp *model.TopUp, clientIP string) {
	cli := GetAlipayClient()
	if cli == nil {
		return
	}
	var q = alipay.TradeQuery{}
	q.OutTradeNo = topUp.TradeNo
	rsp, err := cli.TradeQuery(context.Background(), q)
	if err != nil || rsp == nil || rsp.Code != alipay.CodeSuccess {
		return
	}
	if rsp.TradeStatus != "TRADE_SUCCESS" && rsp.TradeStatus != "TRADE_FINISHED" {
		return
	}
	money, perr := strconv.ParseFloat(string(rsp.TotalAmount), 64)
	if perr != nil || money <= 0 {
		return
	}
	LockOrder(topUp.TradeNo)
	defer UnlockOrder(topUp.TradeNo)
	fresh := model.GetTopUpByTradeNo(topUp.TradeNo)
	if fresh == nil || fresh.Status != common.TopUpStatusPending {
		return
	}
	if cerr := completeAgentTopUp(fresh, money, clientIP); cerr != nil {
		common.SysError("agent topup query-complete failed: " + cerr.Error())
	}
}

type agentClaimCode int

const (
	agentClaimOK agentClaimCode = iota
	agentClaimAlreadyMine
	agentClaimTaken
	agentClaimNotPaid
	agentClaimNotAgentOrder
)

// agentClaimDecision 纯函数判定可否认领(不碰 DB);分组/零金额检查在 handler 内做。
func agentClaimDecision(topUp *model.TopUp, userId int) agentClaimCode {
	if topUp.PaymentProvider != model.PaymentProviderAlipayAgent {
		return agentClaimNotAgentOrder
	}
	if topUp.Status != common.TopUpStatusSuccess {
		return agentClaimNotPaid
	}
	if topUp.UserId == userId {
		return agentClaimAlreadyMine
	}
	if topUp.UserId != 0 {
		return agentClaimTaken
	}
	return agentClaimOK
}

// ClaimAgentTopUp 游客支付后登录/注册,凭 claim_token 把已支付订单绑到自己名下并入账。
func ClaimAgentTopUp(c *gin.Context) {
	var req struct {
		ClaimToken string `json:"claim_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(strings.TrimSpace(req.ClaimToken)) != 32 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	topUp := model.GetTopUpByClaimToken(strings.TrimSpace(req.ClaimToken))
	if topUp == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}
	userId := c.GetInt("id")
	LockOrder(topUp.TradeNo)
	defer UnlockOrder(topUp.TradeNo)
	fresh := model.GetTopUpByTradeNo(topUp.TradeNo)
	if fresh == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}
	switch agentClaimDecision(fresh, userId) {
	case agentClaimAlreadyMine:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"amount": fresh.Amount}})
		return
	case agentClaimOK:
	default:
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单当前不可认领"})
		return
	}
	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	amount := agentQuotaAmountFromMoney(fresh.Money, group)
	if amount <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单当前不可认领"})
		return
	}
	fresh.UserId = userId
	fresh.Amount = amount
	quotaToAdd := int(decimal.NewFromInt(fresh.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
	if err := fresh.Update(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "认领失败"})
		return
	}
	if err := model.IncreaseUserQuota(userId, quotaToAdd, true); err != nil {
		// ponytail: 与 AlipayNotify 同款 latent money leak(Update 已绑用户,加额失败钱在单上不在账上)。
		//   人工兜底: topups 表 status=success 且 user_id>0 但无入账日志的,客服按 Money 补。
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "入账失败，请联系客服"})
		return
	}
	if model.SubmitOrderEvidenceFn != nil {
		go func(in model.SubmitOrderEvidenceInput) {
			if err := model.SubmitOrderEvidenceFn(in); err != nil {
				common.SysError("antchain evidence submit failed: " + err.Error())
			}
		}(model.BuildTopupEvidence(fresh))
	}
	model.RecordTopupLog(userId,
		fmt.Sprintf("使用智能体支付宝充值成功（认领），充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), fresh.Money),
		c.ClientIP(), fresh.PaymentMethod, model.PaymentMethodAlipay)
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"amount": fresh.Amount}})
}
