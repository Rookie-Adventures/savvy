package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Agent 能力层 HTTP 入口（路由前缀 /api/user/agent/ability/*）。
// 鉴权：全部挂 UserAuth——session（浏览器）或 Authorization: Bearer <access_token>（智能体/HTTP 工具）。
// 游客一律不许进：claim_token 只是订单凭据，冒用它等于旁路登录就能读别人余额。
// 分层约定：本文件只做参数解析与响应组装，业务在 service/agent_ability.go。

type agentRedeemRequest struct {
	Code string `json:"code"`
}

type agentRefundApplyRequest struct {
	TradeNo string `json:"out_trade_no"`
	Reason  string `json:"reason"`
}

// parsePosPage 统一分页参数解析（负值/空值兜底，防止 Limit(-1) 之类的注入）
func parsePosPage(c *gin.Context, defaultSize int) (page int, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, perr := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(defaultSize)))
	if perr != nil || pageSize <= 0 {
		pageSize = defaultSize
	}
	return page, pageSize
}

// AgentAbilityBalance GET /api/user/agent/ability/balance
func AgentAbilityBalance(c *gin.Context) {
	view, err := service.AgentQueryBalance(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": view})
}

// AgentAbilityUsage GET /api/user/agent/ability/usage?days=7
func AgentAbilityUsage(c *gin.Context) {
	days, derr := strconv.Atoi(c.DefaultQuery("days", "7"))
	if derr != nil {
		days = 7
	}
	view, err := service.AgentQueryUsage(c.GetInt("id"), days)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": view})
}

// AgentAbilityTopUps GET /api/user/agent/ability/orders
func AgentAbilityTopUps(c *gin.Context) {
	page, pageSize := parsePosPage(c, 10)
	rows, total, err := service.AgentQueryTopUps(c.GetInt("id"), page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"items": rows,
		"total": total,
		"page":  page,
	}})
}

// AgentAbilityRedeem POST /api/user/agent/ability/redeem
func AgentAbilityRedeem(c *gin.Context) {
	var req agentRedeemRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "请提供兑换码"})
		return
	}
	quota, err := service.AgentRedeem(c.GetInt("id"), req.Code)
	if err != nil {
		// ponytail: 兑换失败一律人话回吐（"兑换码无效或已被使用"），不丢 gorm 原文给模型乱转述
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"quota":         quota,
		"quota_display": logger.LogQuota(quota),
	}})
}

// AgentAbilityRefundApply POST /api/user/agent/ability/refund/apply
func AgentAbilityRefundApply(c *gin.Context) {
	var req agentRefundApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	r, err := service.AgentApplyRefund(service.AgentRefundApplyInput{
		UserId:  c.GetInt("id"),
		TradeNo: req.TradeNo,
		Reason:  req.Reason,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"ticket_no":   r.TicketNo,
		"trade_no":    r.TradeNo,
		"amount_yuan": r.AmountYuan,
		"status":      r.Status,
		// 退款是人工原路退回，别让模型暗示即时到账
		"note": "退款申请已受理，款项由人工原路退回，请留意微信支付通知",
	}})
}

// AgentAbilityRefundList GET /api/user/agent/ability/refund/list
func AgentAbilityRefundList(c *gin.Context) {
	page, pageSize := parsePosPage(c, 10)
	rows, total, err := service.AgentListOwnRefunds(c.GetInt("id"), page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"items": rows,
		"total": total,
		"page":  page,
	}})
}

type agentRefundHandleRequest struct {
	Id     int    `json:"id"`
	Status string `json:"status"` // approved / rejected
	Remark string `json:"remark"`
}

// AgentAdminRefundList GET /api/user/agent/ability/refund/admin/list?status=pending
func AgentAdminRefundList(c *gin.Context) {
	page, pageSize := parsePosPage(c, 20)
	rows, total, err := service.AgentAdminListRefunds(c.Query("status"), page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"items": rows,
		"total": total,
		"page":  page,
	}})
}

// AgentAdminRefundHandle POST /api/user/agent/ability/refund/admin/handle
func AgentAdminRefundHandle(c *gin.Context) {
	var req agentRefundHandleRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Id <= 0 ||
		!model.IsValidAgentRefundStatus(req.Status) || req.Status == model.AgentRefundStatusPending {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if err := service.AgentAdminHandleRefund(req.Id, req.Status, c.GetInt("id"), req.Remark); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"id":     req.Id,
		"status": req.Status,
	}})
}
