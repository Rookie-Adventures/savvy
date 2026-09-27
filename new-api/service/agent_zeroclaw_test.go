package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
)

// fakeGateway 起一个最小 ZeroClaw 网关替身:断言握手与帧协议,先回 session_start,
// 收到客户端 message 帧后回放剩余事件(首事件必须是 session_start,对齐真实网关时序)。
func fakeGateway(t *testing.T, wantAuth, wantQuery string, events func() []map[string]any) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Errorf("handshake Authorization = %q, want %q", got, wantAuth)
		}
		if got := r.URL.Path + "?" + r.URL.RawQuery; !strings.HasPrefix(got, wantQuery) {
			t.Errorf("handshake URL = %q, want prefix %q", got, wantQuery)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		all := events()
		if len(all) == 0 || all[0]["type"] != "session_start" {
			t.Errorf("events must start with session_start")
			return
		}
		if err := conn.WriteJSON(all[0]); err != nil {
			return
		}
		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Errorf("read message frame: %v", err)
			return
		}
		if frame["type"] != "message" || frame["content"] == "" {
			t.Errorf("bad client frame: %v", frame)
		}
		for _, ev := range all[1:] {
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		}
	}))
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestAgentChatZeroClawRoundTrip(t *testing.T) {
	origURL, origToken, origAgent := operation_setting.AgentZeroClawURL, operation_setting.AgentZeroClawToken, operation_setting.AgentZeroClawAgent
	defer func() {
		operation_setting.AgentZeroClawURL, operation_setting.AgentZeroClawToken, operation_setting.AgentZeroClawAgent = origURL, origToken, origAgent
	}()

	srv := fakeGateway(t, "Bearer pk-test", "/ws/chat?agent=topup&session_id=sid-old", func() []map[string]any {
		return []map[string]any{
			{"type": "session_start", "session_id": "sid-new", "resumed": true},
			{"type": "chunk", "content": "partial"},
			{"type": "tool_call", "name": "savvy__get_balance"},
			{"type": "done", "full_response": "您的余额为 42"},
		}
	})
	operation_setting.AgentZeroClawURL = wsURL(srv)
	operation_setting.AgentZeroClawToken = "pk-test"
	operation_setting.AgentZeroClawAgent = "topup"

	res, err := AgentChat(context.Background(), AgentChatInput{Prompt: "查余额", SessionID: "sid-old"})
	if err != nil {
		t.Fatalf("AgentChat: %v", err)
	}
	if res.Text != "您的余额为 42" {
		t.Fatalf("text = %q, want done.full_response only", res.Text)
	}
	if res.SessionID != "sid-new" {
		t.Fatalf("session_id = %q, want gateway-returned sid-new", res.SessionID)
	}
}

func TestAgentChatZeroClawErrorFrame(t *testing.T) {
	origURL, origToken, origAgent := operation_setting.AgentZeroClawURL, operation_setting.AgentZeroClawToken, operation_setting.AgentZeroClawAgent
	defer func() {
		operation_setting.AgentZeroClawURL, operation_setting.AgentZeroClawToken, operation_setting.AgentZeroClawAgent = origURL, origToken, origAgent
	}()

	srv := fakeGateway(t, "Bearer pk-test", "/ws/chat?agent=topup", func() []map[string]any {
		return []map[string]any{
			{"type": "session_start", "session_id": "s1"},
			{"type": "error", "message": "provider down"},
		}
	})
	operation_setting.AgentZeroClawURL = wsURL(srv)
	operation_setting.AgentZeroClawToken = "pk-test"
	operation_setting.AgentZeroClawAgent = "topup"

	if _, err := AgentChat(context.Background(), AgentChatInput{Prompt: "hi"}); err == nil ||
		!strings.Contains(err.Error(), "provider down") {
		t.Fatalf("error frame should surface, got %v", err)
	}
}
