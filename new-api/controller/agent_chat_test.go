package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func TestAgentChatRejectsUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origURL, origToken, origAgent := operation_setting.AgentZeroClawURL, operation_setting.AgentZeroClawToken, operation_setting.AgentZeroClawAgent
	operation_setting.AgentZeroClawURL, operation_setting.AgentZeroClawToken, operation_setting.AgentZeroClawAgent = "", "", ""
	defer func() {
		operation_setting.AgentZeroClawURL, operation_setting.AgentZeroClawToken, operation_setting.AgentZeroClawAgent = origURL, origToken, origAgent
	}()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/user/agent/chat", strings.NewReader(`{"prompt":"hi"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	AgentChat(c)
	if !strings.Contains(w.Body.String(), "error") {
		t.Fatalf("unconfigured should return error, got: %s", w.Body.String())
	}
}

func TestAgentChatRejectsEmptyPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/user/agent/chat", strings.NewReader(`{"prompt":"  "}`))
	c.Request.Header.Set("Content-Type", "application/json")

	AgentChat(c)
	if !strings.Contains(w.Body.String(), "error") {
		t.Fatalf("empty prompt should return error, got: %s", w.Body.String())
	}
}
