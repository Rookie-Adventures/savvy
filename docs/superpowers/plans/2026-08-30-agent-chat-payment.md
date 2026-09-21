# 百炼智能体对话下单(Phase 1 体验版)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 new-api 主站新增 `/agent-chat` 页面,用户与百炼智能体对话,智能体经「AI 支付(体验版)」MCP 返回支付链接,前端渲染为带协议勾选的支付卡片。

**Architecture:** 前端新模块 `features/agent-chat`(复用 playground 的 ai-elements 聊天组件)→ 新后端接口 `POST /api/user/agent/chat`(selfRoute,登录态+CriticalRateLimit)→ service 纯 HTTP 调百炼 completion 接口(Host/Key/AppId 三配置项存运营设置,经 OptionMap 落库)。

**Tech Stack:** Go(gin)/React(TS,TanStack Router)/百炼 Application API(非流式,session_id 云端多轮)

## Global Constraints

- JSON 序列化/反序列化一律走 `common/json.go` 的 `common.Marshal`/`common.Unmarshal`,禁用 `encoding/json` 直调(技术红线)
- 分层顺序:`router/ → controller/ → service/`,不跨层
- 数据库无需改动(零 Dialect 适配问题)
- 响应格式对齐现有支付 handler:`gin.H{"message": "success"|"error", "data": ...}`
- API Key 不得出现在任何代码/文档/前端;对话中泄露过的 key 上线前需在百炼控制台轮换
- 支付卡片必须有协议勾选(《用户协议》《隐私政策》),勾选前按钮置灰,每条消息重置
- i18n 新增文案同步 en/ja/fr/ru/vi/zh 六语言
- 代码注释密度贴周边文件(中文短注释 + ponytail 标注简化处)
- 工作目录:`e:\savvy\new-api`(Go 后端)、`e:\savvy\new-api\web\default`(前端)
- 百炼接口事实(已从官方文档验证):
  - 端点:`POST {Host}/api/v1/apps/{AppId}/completion`,Host 为业务空间专属端点(形如 `https://ws-xxx.cn-beijing.maas.aliyuncs.com`),**不是**默认 `dashscope.aliyuncs.com`
  - 请求头:`Authorization: Bearer {Key}`、`Content-Type: application/json`
  - 请求体:`{"input":{"prompt":"...","session_id":"..."},"parameters":{},"debug":{}}`(session_id 可选,云端续会话)
  - 响应体:`{"output":{"finish_reason":"stop","session_id":"...","text":"..."},"usage":{...},"request_id":"..."}`

---

### Task 1: 特性分支 + 后端配置项

**Files:**
- Create: `new-api/setting/operation_setting/agent_bailian.go`
- Modify: `new-api/model/option.go`(OptionMap 注册块,约 L102-106 后;updateOptionMap switch,约 L499 后)
- Test: `new-api/setting/operation_setting/agent_bailian_test.go`
- Modify: `docs/superpowers/specs/2026-08-30-agent-chat-payment-design.md`(修正 API Host 为可配置)

**Interfaces:**
- Produces: `operation_setting.AgentBailianHost / AgentBailianKey / AgentBailianAppId`(string 包级变量)、`operation_setting.IsAgentBailianConfigured() bool` —— Task 2/3 依赖

- [ ] **Step 1: 建特性分支**

```powershell
cd e:\savvy
git checkout -b feature/agent-chat-payment
```

Expected: `Switched to a new branch 'feature/agent-chat-payment'`(工作区已有的未提交文件不动)

- [ ] **Step 2: 写失败测试**

创建 `new-api/setting/operation_setting/agent_bailian_test.go`:

```go
package operation_setting

import "testing"

func TestIsAgentBailianConfigured(t *testing.T) {
	origHost, origKey, origApp := AgentBailianHost, AgentBailianKey, AgentBailianAppId
	defer func() {
		AgentBailianHost, AgentBailianKey, AgentBailianAppId = origHost, origKey, origApp
	}()

	AgentBailianHost, AgentBailianKey, AgentBailianAppId = "", "", ""
	if IsAgentBailianConfigured() {
		t.Fatal("empty config should not be configured")
	}
	AgentBailianHost = "https://ws-x.cn-beijing.maas.aliyuncs.com"
	AgentBailianKey = "sk-xxx"
	AgentBailianAppId = "app1"
	if !IsAgentBailianConfigured() {
		t.Fatal("host+key+appid should be configured")
	}
	AgentBailianAppId = ""
	if IsAgentBailianConfigured() {
		t.Fatal("missing appid should not be configured")
	}
}
```

- [ ] **Step 3: 运行确认失败**

```powershell
cd e:\savvy\new-api; go test ./setting/operation_setting/ -run TestIsAgentBailianConfigured -v
```

Expected: FAIL(`AgentBailianHost undefined` 编译错误)

- [ ] **Step 4: 最小实现**

创建 `new-api/setting/operation_setting/agent_bailian.go`:

```go
package operation_setting

// 百炼智能体(对话下单)配置。Host 为业务空间专属端点(形如 https://ws-xxx.cn-beijing.maas.aliyuncs.com),
// Key 为该业务空间 API Key,AppId 为百炼智能体应用 ID。三者齐全才可用;空则 handler 返回未配置提示。
var (
	AgentBailianHost  = ""
	AgentBailianKey   = ""
	AgentBailianAppId = ""
)

// IsAgentBailianConfigured reports whether admin has filled Bailian agent creds to serve.
func IsAgentBailianConfigured() bool {
	return AgentBailianHost != "" && AgentBailianKey != "" && AgentBailianAppId != ""
}
```

- [ ] **Step 5: 运行确认通过**

```powershell
cd e:\savvy\new-api; go test ./setting/operation_setting/ -run TestIsAgentBailianConfigured -v
```

Expected: PASS

- [ ] **Step 6: OptionMap 注册(落库回读)**

`new-api/model/option.go`,在 `common.OptionMap["AlipayIsProduction"] = strconv.FormatBool(operation_setting.AlipayIsProduction)`(约 L106)之后加:

```go
	// 百炼智能体配置,对齐上方支付宝直连的注册范式,否则 admin 写入后重启即丢
	common.OptionMap["AgentBailianHost"] = operation_setting.AgentBailianHost
	common.OptionMap["AgentBailianKey"] = operation_setting.AgentBailianKey
	common.OptionMap["AgentBailianAppId"] = operation_setting.AgentBailianAppId
```

同文件 `updateOptionMap` switch 内,`case "AlipayAppPrivateKey": ... ` 相邻块(约 L497-499)后加:

```go
	case "AgentBailianHost":
		operation_setting.AgentBailianHost = value
	case "AgentBailianKey":
		operation_setting.AgentBailianKey = value
	case "AgentBailianAppId":
		operation_setting.AgentBailianAppId = value
```

- [ ] **Step 7: 修正设计文档 API 端点描述**

`docs/superpowers/specs/2026-08-30-agent-chat-payment-design.md` 中把
`百炼 API:\`POST https://dashscope.aliyuncs.com/api/v1/apps/{APP_ID}/completion\`,`
替换为
`百炼 API:\`POST {AgentBailianHost}/api/v1/apps/{AppId}/completion\`(Host 为业务空间专属端点,非默认 dashscope.aliyuncs.com),`

- [ ] **Step 8: 编译 + 提交**

```powershell
cd e:\savvy\new-api; go build ./...
git add setting/operation_setting/agent_bailian.go setting/operation_setting/agent_bailian_test.go model/option.go
git commit -m "feat(agent-chat): bailian agent config options with persistence"
```

---

### Task 2: 后端 service(百炼 HTTP 调用)

**Files:**
- Create: `new-api/service/dashscope_agent.go`
- Test: `new-api/service/dashscope_agent_test.go`

**Interfaces:**
- Consumes: `operation_setting.AgentBailianHost/Key/AppId`、`operation_setting.IsAgentBailianConfigured()`
- Produces: `service.AgentChatInput{Prompt, SessionID string}`、`service.AgentChatResult{Text, SessionID string}`、`service.AgentChat(ctx context.Context, in AgentChatInput) (*AgentChatResult, error)` —— Task 3 依赖

- [ ] **Step 1: 写失败测试**

创建 `new-api/service/dashscope_agent_test.go`:

```go
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
```

- [ ] **Step 2: 运行确认失败**

```powershell
cd e:\savvy\new-api; go test ./service/ -run TestAgentChat -v
```

Expected: FAIL(`AgentChat undefined` 编译错误)

- [ ] **Step 3: 实现**

创建 `new-api/service/dashscope_agent.go`:

```go
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
```

- [ ] **Step 4: 运行确认通过**

```powershell
cd e:\savvy\new-api; go test ./service/ -run TestAgentChat -v
```

Expected: 两个测试 PASS

- [ ] **Step 5: 提交**

```powershell
git add service/dashscope_agent.go service/dashscope_agent_test.go
git commit -m "feat(agent-chat): dashscope bailian agent completion service"
```

---

### Task 3: 后端 controller + 路由

**Files:**
- Create: `new-api/controller/agent_chat.go`
- Modify: `new-api/router/api-router.go`(selfRoute 组,约 L117 `/aff_transfer` 前)
- Test: `new-api/controller/agent_chat_test.go`

**Interfaces:**
- Consumes: `service.AgentChat`、`operation_setting.IsAgentBailianConfigured`
- Produces: `controller.AgentChat(c *gin.Context)`,路由 `POST /api/user/agent/chat` —— Task 4 前端依赖

- [ ] **Step 1: 写失败测试**

创建 `new-api/controller/agent_chat_test.go`(对齐 topup_alipay_test.go 范式):

```go
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
	origHost, origKey, origApp := operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId
	operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId = "", "", ""
	defer func() {
		operation_setting.AgentBailianHost, operation_setting.AgentBailianKey, operation_setting.AgentBailianAppId = origHost, origKey, origApp
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
```

- [ ] **Step 2: 运行确认失败**

```powershell
cd e:\savvy\new-api; go test ./controller/ -run TestAgentChat -v
```

Expected: FAIL(`AgentChat undefined`)

- [ ] **Step 3: 实现**

创建 `new-api/controller/agent_chat.go`:

```go
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
```

- [ ] **Step 4: 注册路由**

`new-api/router/api-router.go`,selfRoute 组内 `selfRoute.POST("/aff_transfer", ...)`(约 L117)之前加:

```go
				// ponytail: 对话下单智能体转发,对齐 /alipay/pay L105 范式(登录态+关键限流),无回调无订单落库
				selfRoute.POST("/agent/chat", middleware.CriticalRateLimit(), controller.AgentChat)
```

- [ ] **Step 5: 运行测试 + 编译**

```powershell
cd e:\savvy\new-api; go test ./controller/ -run TestAgentChat -v; go build ./...
```

Expected: PASS + 编译通过

- [ ] **Step 6: 提交**

```powershell
git add controller/agent_chat.go controller/agent_chat_test.go router/api-router.go
git commit -m "feat(agent-chat): POST /api/user/agent/chat endpoint"
```

---

### Task 4: 前端 API helper + 支付链接提取

**Files:**
- Create: `new-api/web/default/src/features/agent-chat/api.ts`
- Create: `new-api/web/default/src/features/agent-chat/lib/pay-links.ts`

**Interfaces:**
- Consumes: `api`(axios 实例,`@/lib/api`)
- Produces: `sendAgentMessage(prompt: string, sessionId: string): Promise<AgentChatResponse>`、`AgentChatResponse{message: string; data: {text: string; session_id: string}}`、`extractPayLinks(text: string): string[]` —— Task 5 依赖

- [ ] **Step 1: api.ts**

```typescript
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'

export type AgentChatResponse = {
  message: string
  data: {
    text: string
    session_id: string
  }
}

/**
 * Send one chat turn to the Bailian agent (non-stream).
 * Mirrors wallet requestAlipayQRPayment's path + skipBusinessError pattern.
 */
export async function sendAgentMessage(
  prompt: string,
  sessionId: string
): Promise<AgentChatResponse> {
  const res = await api.post(
    '/api/user/agent/chat',
    { prompt, session_id: sessionId },
    { skipBusinessError: true } as Record<string, unknown>
  )
  return res.data
}
```

- [ ] **Step 2: lib/pay-links.ts**

```typescript
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
const URL_RE = /https?:\/\/[^\s<>"')\]]+/g

/**
 * Extract payment links from agent reply text.
 * ponytail: 只认包含 alipay 的 URL,漏判无害(退化为普通文本),误判会弹支付卡
 */
export function extractPayLinks(text: string): string[] {
  return Array.from(text.matchAll(URL_RE))
    .map((m) => m[0])
    .filter((u) => /alipay/i.test(u))
}
```

- [ ] **Step 3: 提交**

```powershell
cd e:\savvy\new-api\web\default
git add src/features/agent-chat
git commit -m "feat(agent-chat): frontend api helper and pay-link extraction"
```

---

### Task 5: 前端聊天页 + 支付卡片

**Files:**
- Create: `new-api/web/default/src/features/agent-chat/components/payment-card.tsx`
- Create: `new-api/web/default/src/features/agent-chat/index.tsx`

**Interfaces:**
- Consumes: `sendAgentMessage`、`extractPayLinks`、`@/components/ai-elements/*`(Conversation/Message/Loader,playground 同款)、`@/components/ui/*`
- Produces: `AgentChat` 组件(默认导出形),供 Task 6 路由引用

- [ ] **Step 1: payment-card.tsx**

```tsx
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ExternalLink } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'

type PaymentCardProps = {
  link: string
}

// 支付前同意闸门:每条链接挂载即重置,不跨消息持久化(对齐 wallet payment-confirm-dialog)
export function PaymentCard({ link }: PaymentCardProps) {
  const { t } = useTranslation()
  const [agreed, setAgreed] = useState(false)

  useEffect(() => {
    setAgreed(false)
  }, [link])

  return (
    <div className='bg-card my-2 rounded-lg border p-4'>
      <p className='text-sm font-medium'>
        {t('Alipay payment link generated')}
      </p>
      <label className='mt-3 flex cursor-pointer items-start gap-2'>
        <Checkbox
          checked={agreed}
          onCheckedChange={(v) => setAgreed(v === true)}
          className='mt-0.5'
        />
        <span className='text-muted-foreground text-xs leading-relaxed'>
          {t('I have read and agree to the')}{' '}
          <a
            href='/user-agreement'
            target='_blank'
            rel='noopener noreferrer'
            className='text-primary underline-offset-4 hover:underline'
          >
            {t('User Agreement')}
          </a>{' '}
          {t('and')}{' '}
          <a
            href='/privacy-policy'
            target='_blank'
            rel='noopener noreferrer'
            className='text-primary underline-offset-4 hover:underline'
          >
            {t('Privacy Policy')}
          </a>
        </span>
      </label>
      <Button
        className='mt-3 w-full'
        disabled={!agreed}
        onClick={() => window.open(link, '_blank', 'noopener,noreferrer')}
      >
        <ExternalLink className='mr-2 h-4 w-4' />
        {t('Go to Pay')}
      </Button>
    </div>
  )
}
```

- [ ] **Step 2: index.tsx**

```tsx
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SendHorizonal } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import { Message, MessageContent } from '@/components/ai-elements/message'
import { Loader } from '@/components/ai-elements/loader'
import { PaymentCard } from './components/payment-card'
import { extractPayLinks } from './lib/pay-links'
import { sendAgentMessage } from './api'

type ChatMessage = {
  role: 'user' | 'assistant'
  content: string
}

// 百炼云端会话 id,存 localStorage 续多轮(对齐 playground storage 范式)
const SESSION_KEY = 'agent_chat_session_id'

export function AgentChat() {
  const { t } = useTranslation()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const sessionIdRef = useRef<string>(
    typeof window === 'undefined'
      ? ''
      : (window.localStorage.getItem(SESSION_KEY) ?? '')
  )

  const send = async () => {
    const prompt = input.trim()
    if (!prompt || loading) return
    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: prompt }])
    setLoading(true)
    try {
      const res = await sendAgentMessage(prompt, sessionIdRef.current)
      if (res.message === 'success' && res.data) {
        if (res.data.session_id) {
          sessionIdRef.current = res.data.session_id
          window.localStorage.setItem(SESSION_KEY, res.data.session_id)
        }
        setMessages((prev) => [
          ...prev,
          { role: 'assistant', content: res.data.text },
        ])
      } else {
        setMessages((prev) => [
          ...prev,
          { role: 'assistant', content: t('Agent service is unavailable') },
        ])
      }
    } catch {
      setMessages((prev) => [
        ...prev,
        { role: 'assistant', content: t('Agent service is unavailable') },
      ])
    } finally {
      setLoading(false)
    }
  }

  return (
    <Conversation className='h-full'>
      <ConversationContent className='p-0'>
        <div className='mx-auto w-full max-w-3xl px-4 py-4'>
          {messages.map((m, i) => (
            <Message key={i} from={m.role} className='group flex-row-reverse'>
              <MessageContent>
                {m.content}
                {m.role === 'assistant' &&
                  extractPayLinks(m.content).map((link) => (
                    <PaymentCard key={link} link={link} />
                  ))}
              </MessageContent>
            </Message>
          ))}
          {loading && (
            <Message from='assistant'>
              <MessageContent>
                <Loader>{t('AI assistant is thinking...')}</Loader>
              </MessageContent>
            </Message>
          )}
        </div>
      </ConversationContent>
      <ConversationScrollButton />
      <div className='mx-auto w-full max-w-3xl px-4 pb-4'>
        <div className='flex items-end gap-2'>
          <Textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void send()
              }
            }}
            placeholder={t('Type a message...')}
            rows={2}
            className='resize-none'
          />
          <Button onClick={() => void send()} disabled={loading || !input.trim()}>
            <SendHorizonal className='h-4 w-4' />
          </Button>
        </div>
      </div>
    </Conversation>
  )
}
```

注:若 `Message from` 的类型或 Loader 用法与 playground 实际实现有出入,以 `features/playground/components/playground-chat.tsx` 的真实用法为准微调(结构不变)。

- [ ] **Step 3: 类型检查**

```powershell
cd e:\savvy\new-api\web\default; npx tsc --noEmit
```

Expected: 无新增错误(存量错误忽略)

- [ ] **Step 4: 提交**

```powershell
git add src/features/agent-chat
git commit -m "feat(agent-chat): chat page with consent-gated payment card"
```

---

### Task 6: 路由 + 侧边栏 + 模块开关 + i18n

**Files:**
- Create: `new-api/web/default/src/routes/_authenticated/agent-chat/index.tsx`
- Modify: `new-api/web/default/src/hooks/use-sidebar-data.ts`(chat 组,L59 后)
- Modify: `new-api/web/default/src/features/system-settings/maintenance/config.ts`(chat 段,L59 旁)
- Modify: `new-api/web/default/src/features/system-settings/maintenance/sidebar-modules-section.tsx`(moduleMeta.chat,L95 后)
- Modify: `new-api/web/default/src/features/profile/components/sidebar-modules-card.tsx`(L64 后)
- Modify: `new-api/web/default/src/i18n/locales/{en,zh,ja,fr,ru,vi}.json`

**Interfaces:**
- Consumes: `AgentChat` 组件、`isSidebarModuleEnabled('chat', 'agent_chat')`

- [ ] **Step 1: 路由文件**

创建 `new-api/web/default/src/routes/_authenticated/agent-chat/index.tsx`:

```tsx
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { createFileRoute, redirect } from '@tanstack/react-router'
import { isSidebarModuleEnabled } from '@/lib/nav-modules'
import { Main } from '@/components/layout'
import { AgentChat } from '@/features/agent-chat'

export const Route = createFileRoute('/_authenticated/agent-chat/')({
  beforeLoad: () => {
    if (!isSidebarModuleEnabled('chat', 'agent_chat')) {
      throw redirect({ to: '/dashboard' })
    }
  },
  component: AgentChatPage,
})

function AgentChatPage() {
  return (
    <Main className='p-0'>
      <AgentChat />
    </Main>
  )
}
```

- [ ] **Step 2: 侧边栏入口**

`use-sidebar-data.ts` chat 组 items 里,`url: '/playground'` 项之后加(Bot icon 已在该文件 import):

```typescript
          {
            title: t('Agent Assistant'),
            url: '/agent-chat',
            icon: Bot,
          },
```

- [ ] **Step 3: 模块开关注册(三处,对齐 playground 条目)**

`config.ts` chat 段(`playground: true` 旁)加:

```typescript
      agent_chat: true,
```

`sidebar-modules-section.tsx` `moduleMeta.chat` 里加:

```typescript
      agent_chat: {
        title: t('Agent Assistant'),
        description: t('Chat with the AI agent to top up or subscribe.'),
      },
```

`sidebar-modules-card.tsx` playground 条目后加同构条目:

```typescript
      {
        description: t('Chat with the AI agent to top up or subscribe.'),
        key: 'agent_chat',
        title: t('Agent Assistant'),
      },
```

(若该文件条目结构不同,以其 playground 条目为准做同构复制)

- [ ] **Step 4: i18n 六语言**

新增 key(已存在的 `I have read and agree to the`/`User Agreement`/`and`/`Privacy Policy` 等复用不重加):

| key | en | zh | ja | fr | ru | vi |
|---|---|---|---|---|---|---|
| `Agent Assistant` | Agent Assistant | 智能助手 | エージェントアシスタント | Assistant intelligent | ИИ-ассистент | Trợ lý AI |
| `Go to Pay` | Go to Pay | 去支付 | 支払う | Payer | Оплатить | Thanh toán |
| `Alipay payment link generated` | Alipay payment link generated | 支付链接已生成 | 支払いリンクが生成されました | Lien de paiement généré | Ссылка на оплату создана | Đã tạo liên kết thanh toán |
| `Type a message...` | Type a message... | 输入消息... | メッセージを入力... | Saisir un message... | Введите сообщение... | Nhập tin nhắn... |
| `AI assistant is thinking...` | AI assistant is thinking... | 智能助手思考中... | AIアシスタントが考えています... | L'assistant réfléchit... | ИИ-ассистент думает... | Trợ lý AI đang suy nghĩ... |
| `Agent service is unavailable` | Agent service is unavailable | 智能体服务暂不可用 | エージェントサービスは一時的に利用できません | Service agent temporairement indisponible | Сервис агента временно недоступен | Dịch vụ trợ lý tạm thời không khả dụng |
| `Chat with the AI agent to top up or subscribe.` | Chat with the AI agent to top up or subscribe. | 与智能体对话完成充值或订阅。 | AIエージェントとチャットしてチャージやサブスクライブ。 | Discutez avec l'agent IA pour recharger ou s'abonner. | Общайтесь с ИИ-агентом для пополнения или подписки. | Trò chuyện với trợ lý AI để nạp tiền hoặc đăng ký. |

在每个 locale json 中按字母序插入(对齐现有文件排序习惯)。

- [ ] **Step 5: 类型检查 + 提交**

```powershell
cd e:\savvy\new-api\web\default; npx tsc --noEmit
git add -A src
git commit -m "feat(agent-chat): route, sidebar entry, module toggle and i18n"
```

---

### Task 7: 构建验证 + 配置 + 留痕

**Files:**
- Create: `docs/records/agent-chat-bailian-phase1.md`

**Interfaces:**
- Consumes: 全部前序任务产物

- [ ] **Step 1: 后端全量测试 + 构建**

```powershell
cd e:\savvy\new-api; go test ./setting/... ./service/... ./controller/... ; go build ./...
```

Expected: 全 PASS(存量失败项确认与本任务无关)

- [ ] **Step 2: 前端构建**

```powershell
cd e:\savvy\new-api\web\default; npm run build
```

Expected: 构建成功

- [ ] **Step 3: 配置三项(部署时;本地可先 curl 设置)**

管理员经 option API 写入(对齐支付宝配置的落库机制):

```powershell
# 伪代码示意:key 来自百炼控制台,部署时填真实值;上线前轮换(对话中泄露过)
# PUT /api/option/ { "key": "AgentBailianHost", "value": "https://ws-aw01r8jauk6lu46h.cn-beijing.maas.aliyuncs.com" }
# PUT /api/option/ { "key": "AgentBailianAppId", "value": "cb7afba7673c41d6b06d42172c87a337" }
# PUT /api/option/ { "key": "AgentBailianKey", "value": "<轮换后的新key>" }
```

写入后重启 new-api 容器使配置生效(对齐支付宝密钥重启惯例)。

- [ ] **Step 4: 手工验收(体验版 0.01 元)**

1. 登录主站 → 侧边栏「智能助手」→ 进入 `/agent-chat`
2. 发「我要充值 1 元」→ 智能体回复含支付链接 → 出现支付卡片
3. **未勾选协议时「去支付」按钮置灰**;勾选后可点,新标签打开支付宝收银页
4. 支付 0.01 元(体验版测试商户,资金不可提现)
5. 回对话问「支付成功了吗」→ 智能体调查询支付工具应答
6. 刷新页面继续对话 → session_id 生效,上下文延续
7. 后端未配置 key 时访问 → 返回「当前管理员未配置智能体信息」

- [ ] **Step 5: 留痕文档**

创建 `docs/records/agent-chat-bailian-phase1.md`(一问题一 md 规范:现象/根因→方案/改动/验证/限制/尾巴):

```markdown
# 百炼智能体对话下单 Phase 1(体验版)落地记录

## 现象/目标
用户希望在主站对话中直接创建充值/订阅订单(对话即下单)。

## 方案
百炼智能体(应用 ID cb7afba7...)+「AI 支付(体验版)」MCP;
new-api 新增 /api/user/agent/chat 转发接口(配置 Host/Key/AppId 走 OptionMap);
前端 /agent-chat 页复用 ai-elements 聊天组件,回复中的 alipay 链接渲染为
带协议勾选的支付卡片。

## 改动清单
- setting/operation_setting/agent_bailian.go(配置项)
- model/option.go(落库注册)
- service/dashscope_agent.go(百炼 HTTP 调用,60s 超时)
- controller/agent_chat.go + router(POST /api/user/agent/chat,CriticalRateLimit)
- web features/agent-chat(聊天页+支付卡片+api+pay-links)
- 路由/侧边栏/模块开关/i18n 六语言

## 验证
[贴 0.01 元全流程截图与结论:勾选门禁/支付链接/查询确认/多轮会话]

## 限制
- 体验版资金进测试商户,不可提现;正式收款需企业支付宝扫码签约换生产密钥
- 非流式输出;支付成功不加钱包额度(记账桥是 Phase 2)
- 无管理后台表单,配置经 option API + 重启生效

## 尾巴
- Phase 2:AI 收正式签约、记账桥(轮询查询支付→加额度)、userId 透传、流式
- 上线前轮换泄露过的 DASHSCOPE key
```

- [ ] **Step 6: 最终提交**

```powershell
git add docs/records/agent-chat-bailian-phase1.md docs/superpowers/specs/2026-08-30-agent-chat-payment-design.md
git commit -m "docs(agent-chat): phase-1 record and spec host fix"
```

---

## Self-Review 结论

- Spec 覆盖:后端配置/service/controller/路由、前端页面/支付卡片/同意勾选、i18n、验证留痕均有对应任务;Phase 2 内容按 spec 明确不做 ✓
- 无占位符:所有代码步骤含完整代码;唯一灵活点(Task 5 注)是 ai-elements 组件 props 以 playground 真实用法为准,已指明确切参照文件 ✓
- 类型一致性:`AgentChatInput/AgentChatResult/AgentChat` 在 Task 2 定义、Task 3 消费;前端 `sendAgentMessage/extractPayLinks/PaymentCard/AgentChat` 在 Task 4/5 定义、Task 5/6 消费,命名一致 ✓
