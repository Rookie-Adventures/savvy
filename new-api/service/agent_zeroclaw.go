package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
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

// AgentChat 经 ZeroClaw 网关 /ws/chat 完成一轮对话:每次请求一条连接,
// 收 session_start 拿到实际会话 ID,发 message 帧,阻塞到 done 帧取 full_response。
// 会话历史由 ZeroClaw 按 session_id 云端持有(同百炼语义,前端无感)。
func AgentChat(ctx context.Context, in AgentChatInput) (*AgentChatResult, error) {
	u := strings.TrimRight(operation_setting.AgentZeroClawURL, "/") + "/ws/chat?agent=" +
		url.QueryEscape(operation_setting.AgentZeroClawAgent)
	if in.SessionID != "" {
		u += "&session_id=" + url.QueryEscape(in.SessionID)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+operation_setting.AgentZeroClawToken)

	dialCtx, cancel := context.WithTimeout(ctx, agentChatTimeout)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, u, header)
	if err != nil {
		return nil, fmt.Errorf("zeroclaw ws dial: %w", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(agentChatTimeout))

	result := &AgentChatResult{SessionID: in.SessionID}
	// setup 阶段可能先发 connected 等无关帧,等到 session_start 再发言。
	for {
		var frame struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
		}
		if err := conn.ReadJSON(&frame); err != nil {
			return nil, fmt.Errorf("zeroclaw await session_start: %w", err)
		}
		if frame.Type == "session_start" {
			if frame.SessionID != "" {
				result.SessionID = frame.SessionID
			}
			break
		}
	}

	payload, err := common.Marshal(map[string]any{"type": "message", "content": in.Prompt})
	if err != nil {
		return nil, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return nil, fmt.Errorf("zeroclaw write: %w", err)
	}

	for {
		var event struct {
			Type         string `json:"type"`
			FullResponse string `json:"full_response"`
			Message      string `json:"message"`
		}
		if err := conn.ReadJSON(&event); err != nil {
			return nil, fmt.Errorf("zeroclaw await done: %w", err)
		}
		switch event.Type {
		case "done":
			result.Text = event.FullResponse
			return result, nil
		case "error":
			return nil, fmt.Errorf("zeroclaw agent error: %s", event.Message)
		}
		// chunk/thinking/tool_call 等中间帧对非流式前端无意义,跳过。
	}
}
