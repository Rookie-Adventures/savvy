package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// ponytail: prompt 上限 4000 字符对齐一般聊天输入,超长直接拒,不做截断(截断会改变智能体意图)
const agentChatPromptMaxLen = 4000

type AgentChatRequest struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id"`
}

// AgentChat forwards a chat turn to the Bailian agent app (non-stream).
func AgentChat(c *gin.Context) {
	var req AgentChatRequest
	if err := c.ShouldBindJSON(&req); err != nil ||
		strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > agentChatPromptMaxLen {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if !operation_setting.IsAgentBailianConfigured() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置智能体信息"})
		return
	}
	result, err := service.AgentChat(c.Request.Context(), service.AgentChatInput{
		Prompt:    strings.TrimSpace(req.Prompt),
		SessionID: req.SessionID,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "智能体服务暂不可用"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": result})
}
