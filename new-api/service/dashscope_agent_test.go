package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestAgentChatSessionRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/apps/app123/completion" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key123" {
			t.Errorf("unexpected auth: %s", got)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := common.Unmarshal(raw, &body); err != nil {
			t.Fatalf("bad request body: %v", err)
		}
		input, _ := body["input"].(map[string]any)
		if input["prompt"] != "充值1元" {
			t.Errorf("unexpected prompt: %v", input["prompt"])
		}
		if input["session_id"] != "sess1" {
			t.Errorf("unexpected session_id: %v", input["session_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"finish_reason":"stop","session_id":"sess2","text":"支付链接 https://render.alipay.com/p/pay?x=1"},"request_id":"r1"}`))
	}))
	defer srv.Close()

	origHost, origKey, origApp := operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId
	operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId = srv.URL, "key123", "app123"
	defer func() {
		operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId = origHost, origKey, origApp
	}()

	res, err := AgentChat(context.Background(), AgentChatInput{Prompt: "充值1元", SessionID: "sess1"})
	if err != nil {
		t.Fatalf("AgentChat failed: %v", err)
	}
	if res.Text == "" || res.SessionID != "sess2" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestAgentChatUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	origHost, origKey, origApp := operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId
	operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId = srv.URL, "key123", "app123"
	defer func() {
		operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId = origHost, origKey, origApp
	}()

	if _, err := AgentChat(context.Background(), AgentChatInput{Prompt: "hi"}); err == nil {
		t.Fatal("upstream 500 should return error")
	}
}
