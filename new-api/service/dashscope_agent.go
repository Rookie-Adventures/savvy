package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 智能体可能连环调用支付 MCP 工具(创建支付→查询支付),60s 兜底
const agentChatTimeout = 60 * time.Second

type AgentChatInput struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id,omitempty"`
}

type AgentChatResult struct {
	Text      string `json:"text"`
	SessionID string `json:"session_id"`
}

// AgentChat 调用百炼智能体应用 completion 接口(非流式)。
// session_id 非空时百炼自动加载云端会话历史,实现多轮上下文。
func AgentChat(ctx context.Context, in AgentChatInput) (*AgentChatResult, error) {
	host := strings.TrimRight(operation_setting.AgentBailianHost, "/")
	url := fmt.Sprintf("%s/api/v1/apps/%s/completion", host, operation_setting.AgentBailianAppId)
	input := map[string]any{"prompt": in.Prompt}
	if in.SessionID != "" {
		input["session_id"] = in.SessionID
	}
	payload, err := common.Marshal(map[string]any{
		"input":      input,
		"parameters": map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+operation_setting.AgentBailianKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: agentChatTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bailian agent upstream status %d", resp.StatusCode)
	}
	var out struct {
		Output struct {
			Text      string `json:"text"`
			SessionID string `json:"session_id"`
		} `json:"output"`
	}
	if err := common.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &AgentChatResult{Text: out.Output.Text, SessionID: out.Output.SessionID}, nil
}
