package controller

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

// 微信智能体代触发原生充值（服务包）：
// 智能体代用户创建微信 Native 充值订单 → 用户微信扫码支付（普通微信支付，无需 AI 专属卡/weixinpay 插件）
// → 现有 /api/user/wechat/notify 回调入账（登录单）或游客单标记等认领（claim_token，复用 agent_topup 链路）。
// 金额由智能体按用户对话申报（1~5000 元），入账以渠道实付为准（completeAgentTopUp 同构）。

type AgentWechatTopUpRequest struct {
	AmountYuan float64 `json:"amount_yuan"`
}

// CreateAgentWechatTopUp POST /api/agent/wechat/topup/create
// 返回 code_url（微信 Native 支付二维码内容）+ claim_token + 状态轮询地址。
func CreateAgentWechatTopUp(c *gin.Context) {
	var req AgentWechatTopUpRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.AmountYuan <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.AmountYuan < 1 || req.AmountYuan > 5000 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额需在 1~5000 元之间"})
		return
	}
	svc := GetWechatClient()
	if svc == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	userId := c.GetInt("id") // TryUserAuth：服务号/已登录用户直接绑定；游客为 0 走认领
	claimToken, err := newClaimToken()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "下单失败"})
		return
	}
	// WXAGT(5) + 14 位时间戳 + 10 位随机 = 29 位 ≤ 32
	outTradeNo := fmt.Sprintf("WXAGT%s%s", time.Now().Format("20060102150405"), common.GetRandomString(10))
	topUp := &model.TopUp{
		UserId:          userId,
		TradeNo:         outTradeNo,
		ClaimToken:      claimToken,
		Money:           req.AmountYuan,
		PaymentMethod:   model.PaymentMethodWechat,
		PaymentProvider: model.PaymentProviderWechatAgent,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	resp, _, err := svc.Prepay(context.Background(), native.PrepayRequest{
		Appid:       core.String(operation_setting.WechatAppId),
		Mchid:       core.String(operation_setting.WechatMchID),
		Description: core.String("栗橙科技-服务包"),
		OutTradeNo:  core.String(outTradeNo),
		NotifyUrl:   core.String(service.GetCallbackAddress() + "/api/user/wechat/notify"),
		Amount: &native.Amount{
			// ponytail: float→分 math.Round 防截断（同 topup_wechat.go 先例）
			Total:    core.Int64(int64(math.Round(req.AmountYuan * 100))),
			Currency: core.String("CNY"),
		},
	})
	if err != nil {
		logger.LogError(c, fmt.Sprintf("wechat agent topup prepay failed: trade_no=%s err=%v", outTradeNo, err))
		_ = model.UpdatePendingTopUpStatus(outTradeNo, model.PaymentProviderWechatAgent, common.TopUpStatusFailed)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	data := gin.H{
		"code_url":     *resp.CodeUrl, // 微信 Native 支付二维码内容（转二维码给用户扫）
		"out_trade_no": outTradeNo,
		"amount_yuan":  req.AmountYuan,
		"claim_token":  claimToken,
		"status_url":   fmt.Sprintf("/api/agent/topup/status?claim_token=%s", claimToken),
	}
	if userId > 0 {
		// 登录单：付款后由 /api/user/wechat/notify 直接入账，无需认领
		data["bind_mode"] = "auto"
	} else {
		// 游客单：付款后凭 claim_token 认领（ClaimAgentTopUp）
		data["bind_mode"] = "claim"
		data["claim_url"] = system_setting.ServerAddress + "/agent"
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": data})
}

// tryCompleteWechatAgentByQuery 微信智能体游客单的查单兜底（对齐 tryCompleteAgentTopUpByQuery 的支付宝分支）：
// pending 超 10s 的单按 out_trade_no 向微信查单，SUCCESS 则完成（游客单只标记等认领）。
func tryCompleteWechatAgentByQuery(topUp *model.TopUp, clientIP string) {
	tx, err := wechatQueryOrderFn(topUp.TradeNo)
	if err != nil || tx == nil || tx.TradeState == nil || *tx.TradeState != "SUCCESS" {
		return
	}
	totalFen := int64(0)
	if tx.Amount != nil && tx.Amount.Total != nil {
		totalFen = *tx.Amount.Total
	}
	if totalFen <= 0 {
		return
	}
	auditPtr, aerr := wechatAuditFromTx(tx)
	if aerr != nil || auditPtr == nil {
		return
	}
	audit := *auditPtr
	LockOrder(topUp.TradeNo)
	defer UnlockOrder(topUp.TradeNo)
	fresh := model.GetTopUpByTradeNo(topUp.TradeNo)
	if fresh == nil || fresh.Status != common.TopUpStatusPending {
		return
	}
	if cerr := completeAgentTopUp(fresh, float64(totalFen)/100, audit, clientIP, model.PaymentProviderWechatAgent); cerr != nil {
		common.SysError("wechat agent topup query-complete failed: " + cerr.Error())
	}
}
