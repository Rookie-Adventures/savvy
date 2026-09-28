package controller

// X402「已付款却未履约」自助恢复链路。
//
// 问题来源（2026-09-28 真机实测）：用户在手机浏览器（未登录站点）让 Agent 充值，微信扣款成功，
// 但 Agent 换了会话后拿不到 payment_code —— 而 payment_code 是 A-4 校验「谁付的钱」的唯一凭证，
// 于是重试永远 401，钱停在商户账户里，用户侧没有任何自助出口，只能开工单。
//
// 三件套（按响应速度排序）：
//  1. 建单即预生成 claim_token 并塞进 402 响应体（见 handleSkillPayTopUpFirstRequest）：
//     认领链接在支付发生前就交付到用户手上，会话丢失不再是资金问题。
//  2. GET /api/skill/recover?claim_token=xxx：匿名可达，凭据即 32 位 crypto/rand token。
//     拿它向微信查单完成履约，用户点开就得到「已到账/去认领」，不必回到 Agent 会话。
//  3. 后台兜底任务：下单 5 分钟后仍未履约的单定时查单 —— 已支付就补履约（老客户直入账后
//     经服务号确认），超过 Native 订单有效期(2h)还没付就关单，避免死单被无限轮询。
//
// 安全边界：只有 topup 参与自动恢复。qa 的履约物是一次 AI 回答，绕过 payment_code 等于白送。

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	// skillPaySweepMinAgeSec 兜底任务 5 分钟后才出手：正常流程是付完款立刻带码重试，
	// 太早介入会抢掉「服务号 webview 登录态直入账」这条更优路径。
	skillPaySweepMinAgeSec = 300
	// skillPayOrderAgeCloseSec 微信 Native code_url 有效期 2 小时，超期仍未支付即可关单。
	skillPayOrderAgeCloseSec = 2*3600 + 60
	skillPaySweepBatch       = 20
	skillPaySweepTick        = 5 * time.Minute
)

// skillPayFulfillByQuery 纯查单履约：不问 payment_code，以微信查单结果为唯一付款事实。
// 用于恢复入口与兜底任务（这两处用户手上只有 claim_token，没有会话凭证）。
// 身份判定仍复用 fulfillSkillPayTopUp，sessionUserId 固定 0 —— 这一路只认付款人 openid。
// 返回 (content, tradeState, error)：未支付不算错误，state 交给调用方决策（补履约 or 关单）。
func skillPayFulfillByQuery(ctx context.Context, clientIP string, order *model.SkillPayOrder) (string, string, error) {
	if order == nil || order.Fulfilled {
		return "", "", fmt.Errorf("order not recoverable")
	}
	if order.Kind != model.SkillPayKindTopUp {
		return "", "", fmt.Errorf("kind %s not recoverable", order.Kind)
	}
	tx, err := wechatQueryOrderFn(order.OutTradeNo)
	if err != nil {
		return "", "", fmt.Errorf("query: %w", err)
	}
	state := "UNKNOWN"
	if tx != nil && tx.TradeState != nil {
		state = *tx.TradeState
	}
	if state != "SUCCESS" {
		return "", state, nil
	}
	transactionId := ""
	if tx.TransactionId != nil {
		transactionId = *tx.TransactionId
	}
	payerOpenid := ""
	if tx.Payer != nil && tx.Payer.Openid != nil {
		payerOpenid = *tx.Payer.Openid
	}
	LockOrder(order.OutTradeNo)
	defer UnlockOrder(order.OutTradeNo)

	// 锁内重读：正常重试路径可能就在这几毫秒里刚履约完
	fresh := model.GetSkillPayOrderByTradeNo(order.OutTradeNo)
	if fresh == nil {
		return "", "", fmt.Errorf("order vanished")
	}
	if fresh.Fulfilled {
		content, _ := model.GetSkillPayOrderFulfilledContent(fresh.OutTradeNo)
		return content, state, nil
	}
	if payerOpenid == "" {
		payerOpenid = fresh.PayerOpenid // 查单没带就吃回调已记的身份
	}
	_ = model.MarkSkillPayOrderPaid(fresh.OutTradeNo, transactionId, payerOpenid)
	_ = model.BindSkillPayPayerOpenid(fresh.OutTradeNo, payerOpenid)

	paidFen := int64(0)
	if tx.Amount != nil && tx.Amount.Total != nil {
		paidFen = *tx.Amount.Total
	}
	content, err := fulfillSkillPayTopUp(ctx, clientIP, 0, fresh.OutTradeNo, transactionId,
		float64(paidFen)/100, payerOpenid, fresh.ClaimToken)
	if err != nil {
		return "", state, err
	}
	rows, err := model.FulfillSkillPayOrder(fresh.OutTradeNo, transactionId, content)
	if err != nil {
		return "", state, err
	}
	if rows == 0 {
		content, _ = model.GetSkillPayOrderFulfilledContent(fresh.OutTradeNo)
	}
	return content, state, nil
}

// SkillPayRecover GET /api/skill/recover?claim_token=xxx（匿名 + Critical 限频）。
// claim_token 是 128-bit crypto/rand 凭据，本身就是这笔钱的钥匙，所以不需要登录态；
// 回包只给结论（状态/到账文案/认领链接），不回显额度换算与身份细节。
func SkillPayRecover(c *gin.Context) {
	if !operation_setting.IsSkillPayConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SKILLPAY_DISABLED", "message": "SkillPay 未启用"})
		return
	}
	token := strings.TrimSpace(c.Query("claim_token"))
	order := model.GetSkillPayOrderByClaimToken(token)
	if order == nil || order.Kind != model.SkillPayKindTopUp {
		// 不区分「不存在」与「非技能单」：免得把凭据空间变成订单枚举器
		c.JSON(http.StatusNotFound, gin.H{"code": "ORDER_NOT_FOUND", "message": "订单不存在"})
		return
	}
	claimURL := buildAgentClaimUrl(system_setting.ServerAddress, token, order.OutTradeNo)
	// 已履约（多半是正常重试路径刚做完）：直接回缓存，不再打扰微信查单
	if order.Fulfilled {
		cached, _ := model.GetSkillPayOrderFulfilledContent(order.OutTradeNo)
		c.JSON(http.StatusOK, gin.H{
			"code": "PAID", "message": "已收款", "fulfilled": true,
			"content": cached, "claim_url": claimURL,
			"credit_state": skillPayCreditState(model.GetTopUpByTradeNo(order.OutTradeNo)),
		})
		return
	}
	content, state, err := skillPayFulfillByQuery(c.Request.Context(), c.ClientIP(), order)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("skillpay recover failed: out_trade_no=%s err=%v", order.OutTradeNo, err))
		c.JSON(http.StatusOK, gin.H{"code": "PENDING", "message": "订单处理中，请稍后刷新", "trade_state": state})
		return
	}
	if state != "SUCCESS" {
		c.JSON(http.StatusOK, gin.H{"code": "NOT_PAID", "message": "尚未收到付款，可在对话里重新发起支付", "trade_state": state})
		return
	}
	fresh := model.GetSkillPayOrderByTradeNo(order.OutTradeNo)
	tu := model.GetTopUpByTradeNo(order.OutTradeNo)
	c.JSON(http.StatusOK, gin.H{
		"code": "PAID", "message": "已收款", "trade_state": state,
		"fulfilled":    fresh != nil && fresh.Fulfilled,
		"content":      content, // 直入账=到账文案；匿名=含 claim_token 的认领说明
		"claim_url":    claimURL,
		"credit_state": skillPayCreditState(tu),
	})
}

// skillPayCreditState 恢复入口唯一需要外传的结论：已入账 / 待认领 / 未知。
func skillPayCreditState(tu *model.TopUp) string {
	switch {
	case tu == nil:
		return "unknown"
	case tu.UserId > 0:
		return "credited"
	default:
		return "need_claim"
	}
}

var (
	skillPaySweepOnce sync.Once
	skillPaySweepRun  atomic.Bool
)

// StartSkillPayRecoveryTask 主节点定时扫「超过 5 分钟仍未履约」的充值单。
// 把死角从「客服工单级」降到「不存在」的最后一级。
func StartSkillPayRecoveryTask() {
	skillPaySweepOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("skillpay recovery task started: tick=%s minAge=%ds", skillPaySweepTick, skillPaySweepMinAgeSec))
			ticker := time.NewTicker(skillPaySweepTick)
			defer ticker.Stop()
			for range ticker.C {
				runSkillPaySweepOnce()
			}
		})
	})
}

func runSkillPaySweepOnce() {
	if !skillPaySweepRun.CompareAndSwap(false, true) {
		return
	}
	defer skillPaySweepRun.Store(false)
	if !operation_setting.IsSkillPayConfigured() {
		return
	}
	orders, err := model.GetUnfulfilledSkillPayTopUpOrders(skillPaySweepBatch, skillPaySweepMinAgeSec)
	if err != nil {
		common.SysError("skillpay sweep query failed: " + err.Error())
		return
	}
	for _, o := range orders {
		_, state, ferr := skillPayFulfillByQuery(context.Background(), "127.0.0.1", o)
		switch {
		case ferr != nil:
			common.SysError(fmt.Sprintf("skillpay sweep fulfill failed: out_trade_no=%s err=%v", o.OutTradeNo, ferr))
		case state == "SUCCESS":
			logger.LogInfo(context.Background(), fmt.Sprintf("skillpay sweep fulfilled stalled order: out_trade_no=%s", o.OutTradeNo))
			pushSweptResult(o.OutTradeNo)
		case time.Now().Unix()-o.CreateTime > skillPayOrderAgeCloseSec:
			// 订单早已过期又没付：关单，停止无谓轮询
			if cerr := model.MarkSkillPayOrderSweptClosed(o.OutTradeNo); cerr != nil {
				common.SysError("skillpay sweep close failed: " + cerr.Error())
			}
		}
	}
}

// pushSweptResult 兜底履约成功后经服务号客服消息确认到账。
//
// 只推「已直入账」的单：客服消息只能用服务号 openid 发，而 X402 解出的付款人 openid 属于
// Native AppID 空间，两个空间不能互换（跨 App 要靠 unionid）。因此这里靠单上的 user_id 顺藤
// 摸到该用户绑过的 mp_openid 才发；匿名付款人（openid 没命中站内用户）没有可信送达身份，
// 钱挂在 TopUp 上等建单时就已交付的 claim_url 自助认领 —— 不猜身份、不乱发消息。
// notify 重投与多轮 sweep 共用 mp_pushed_at 原子占位，一笔最多推一条。
func pushSweptResult(outTradeNo string) {
	tu := model.GetTopUpByTradeNo(outTradeNo)
	if tu == nil || tu.UserId <= 0 {
		return // 匿名待认领单：无可信送达身份，交给 claim_url
	}
	mpOpenid := model.GetUserMpOpenid(tu.UserId)
	if mpOpenid == "" {
		return
	}
	if !model.ClaimSkillPayMpPushOnce(outTradeNo) {
		return
	}
	text := fmt.Sprintf("✅ 你的 Savvy 充值 ¥%.2f 已自动到账，可在「我的钱包」查看余额。", tu.Money)
	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := service.SendCustomTextMessage(ctx, mpOpenid, text); err != nil {
			common.SysError("skillpay result push failed: " + err.Error())
		}
	})
}
