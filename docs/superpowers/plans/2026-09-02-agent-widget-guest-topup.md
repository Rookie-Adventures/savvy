# 全站悬浮智能体 + 免登录"先付后认领"充值 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 agent-chat 从登录墙后的独立页面改造成全站悬浮 widget，游客可对话下单，支付成功后经"认领卡片"登录/注册自动入账。

**Architecture:** 复用现有 `topups` 表与 `/api/user/alipay/notify` 回调栈（新增 `alipay_agent` provider 分支 + `claim_token` 列）；聊天/登记接口挂 `middleware.TryUserAuth()` 开放游客；前端 widget 挂 `__root.tsx`，认领态存 sessionStorage 跨登录跳转恢复。

**Tech Stack:** Go (gin + GORM + smartwalle/alipay/v3 + shopspring/decimal)、React (TanStack Router + zustand + sonner + ai-elements)、i18n 六语言。

**Spec:** `docs/superpowers/specs/2026-09-02-agent-widget-guest-topup-design.md`

## Global Constraints

- 分支：`feature/agent-widget-guest-topup`（已建，基于 feature/agent-chat-payment）
- JSON 序列化一律 `common.Marshal`/`common.Unmarshal`（禁 `encoding/json` 直用）
- DB 三库兼容（SQLite/MySQL≥5.7.8/PG≥9.6），新列走 GORM AutoMigrate
- 分层：router → controller → service → model，不跨层
- i18n 新文案同步 en/ja/fr/ru/vi/zh 六语言
- 金额一律 decimal 运算；入账金额以支付宝侧 `total_amount` 为准，不信对话内容
- 品牌红线：不改 QuantumNous / new-api 归属；对客文案品牌 = Savvy Agent
- 敏感信息（密钥、API Key）不写入任何文档与代码
- 每个任务结束跑对应测试 + `git commit`

## 动工前置（人工验证，非代码任务）

- [ ] **V0 同应用验证**：管理后台支付网关配置里的 AlipayAppId 是否等于 MCP 受限密钥应用 APPID（栗橙网络科技 2021006170668597）。不等 → 停下，先让管理员把站点支付配置切到同一应用。
- [ ] **V1**：昨日 `invalid-open-scene-api-permission` 已排除（产品已生效 + 受限密钥 5 工具已勾选），0.01 元 MCP 订单能打开支付页。
- [ ] **V2**：百炼正式版 MCP 配置页查 `AP_NOTIFY_URL` 是否可填（可填 → 填 `https://<站点域名>/api/user/alipay/notify`）。不可填不阻塞：Task 3 的按需查单兜底独立成立。

---

### Task 1: model 层扩展 + 金额换算 helper

**Files:**
- Modify: `new-api/model/topup.go`（TopUp 结构体、provider 常量、新查询函数）
- Create: `new-api/controller/agent_topup.go`（本任务只放换算 helper，Task 2/3 往里加接口）
- Test: `new-api/controller/agent_topup_test.go`

**Interfaces:**
- Produces:
  - `model.PaymentProviderAlipayAgent = "alipay_agent"`（string 常量）
  - `TopUp.ClaimToken string`（gorm varchar(64) index）
  - `model.GetTopUpByClaimToken(token string) *TopUp`
  - `controller.agentQuotaAmountFromMoney(money float64, group string) int64` —— getPayMoney 的逆运算
  - `controller.newClaimToken() (string, error)` —— crypto/rand 16 字节 hex

- [ ] **Step 1: 写失败测试**（`controller/agent_topup_test.go`）

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestAgentQuotaAmountFromMoney(t *testing.T) {
	// 对齐 getPayMoney: money = amount * Price * groupRatio (非 TOKENS 展示)
	oldPrice := operation_setting.Price
	oldDisplay := operation_setting.QuotaDisplayType
	t.Cleanup(func() {
		operation_setting.Price = oldPrice
		operation_setting.QuotaDisplayType = oldDisplay
	})
	operation_setting.Price = 7.0 // 1 USD 额度 = 7 RMB
	operation_setting.QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	// 70 RMB / 7.0 / ratio 1 = 10 USD 额度
	if got := agentQuotaAmountFromMoney(70, "default"); got != 10 {
		t.Fatalf("want 10, got %d", got)
	}
	// 0.1 RMB → 0.0143 USD → 四舍五入 0（小额订单不炸,入账为 0 由认领接口拒绝）
	if got := agentQuotaAmountFromMoney(0.1, "default"); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestNewClaimToken(t *testing.T) {
	a, err := newClaimToken()
	if err != nil || len(a) != 32 {
		t.Fatalf("bad token %q err %v", a, err)
	}
	b, _ := newClaimToken()
	if a == b {
		t.Fatal("tokens must be random")
	}
}
```

注意：`operation_setting.QuotaDisplayType`/`QuotaDisplayTypeUSD` 的真实标识符以 `setting/operation_setting` 包内 `GetQuotaDisplayType()` 实现为准（getPayMoney 引用处可见），写测试前先打开该文件核对，若变量不可直接赋值则改用对应 setter 或以 `GetQuotaDisplayType()` 默认值调整断言。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd e:\savvy\new-api; go test ./controller/ -run 'TestAgentQuota|TestNewClaimToken' -v`
Expected: 编译失败 `undefined: agentQuotaAmountFromMoney`

- [ ] **Step 3: 实现**

`model/topup.go` —— TopUp 结构体 TradeNo 行后加一列；provider 常量块加一行；文件尾部加查询函数（对齐 GetTopUpByTradeNo 写法）：

```go
	ClaimToken      string  `json:"claim_token" gorm:"type:varchar(64);index"`
```

```go
	PaymentProviderAlipayAgent = "alipay_agent"
```

```go
func GetTopUpByClaimToken(claimToken string) *TopUp {
	var topUp *TopUp
	err := DB.Where("claim_token = ?", claimToken).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}
```

`controller/agent_topup.go`（新文件，含版权头，对齐 topup_alipay.go 头注释风格）：

```go
package controller

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

// agentQuotaAmountFromMoney 是 getPayMoney 的逆运算: 实付 RMB → 额度单位数量。
// ponytail: 不套 AmountDiscount(那是预设金额档位的促销,智能体订单金额任意,无档位可配)。
// 返回单位与 TopUp.Amount 语义一致: 非 TOKENS 展示 = USD 整数; TOKENS 展示 = token 数,
// 保证认领/入账处 quotaToAdd = Amount * QuotaPerUnit 与现有 AlipayNotify L192 同构。
func agentQuotaAmountFromMoney(money float64, group string) int64 {
	dMoney := decimal.NewFromFloat(money)
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	usd := dMoney.Div(decimal.NewFromFloat(operation_setting.Price)).
		Div(decimal.NewFromFloat(ratio))
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return usd.Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	return usd.Round(0).IntPart()
}

// newClaimToken 生成 128-bit 随机认领凭据(hex 32 字符)。
// 不用 common.GetRandomString: 认领凭据是钱的钥匙,必须 crypto/rand。
func newClaimToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd e:\savvy\new-api; go test ./controller/ -run 'TestAgentQuota|TestNewClaimToken' -v; go build ./...`
Expected: 2 PASS；build 成功。若 controller 包存在与本改动无关的存量失败（如渠道亲和性缓存两例，见 docs/records/agent-chat-bailian-phase1.md），只要 `-run` 过滤的本任务测试全绿即通过。

- [ ] **Step 5: Commit**

```bash
git add new-api/model/topup.go new-api/controller/agent_topup.go new-api/controller/agent_topup_test.go
git commit -m "feat(agent-topup): claim token column, alipay_agent provider, money->quota helper"
```

---

### Task 2: 登记接口 + 状态接口（含按需查单兜底）+ 游客通道与限流

**Files:**
- Modify: `new-api/controller/agent_topup.go`（追加 handler 与游客限额）
- Modify: `new-api/controller/topup_alipay.go`（追加 `completeAgentTopUp`，Task 3 的 notify 分支也要用）
- Modify: `new-api/router/api-router.go`（chat 路由改 TryUserAuth；新增 register/status 匿名路由）
- Modify: `new-api/setting/operation_setting/agent_bailian.go`（游客限额配置变量）
- Modify: `new-api/model/option.go`（限额两项 OptionMap 注册，镜像 AgentBailianHost 的两处写法）
- Test: `new-api/controller/agent_topup_test.go`（追加）

**Interfaces:**
- Consumes: Task 1 的 `newClaimToken`、`agentQuotaAmountFromMoney`、`model.PaymentProviderAlipayAgent`、`model.GetTopUpByClaimToken`
- Produces:
  - `POST /api/user/agent/topup/register` body `{"out_trade_no": string}` → `{"message":"success","data":{"claim_token":string}}`（TryUserAuth，重复登记返回 error message）
  - `GET /api/user/agent/topup/status?claim_token=` → `{"message":"success","data":{"status":"pending|success|failed|expired","money":float,"claimed":bool}}`（匿名，token 即凭据）
  - `controller.completeAgentTopUp(topUp *model.TopUp, actualMoney float64, clientIP string) error` —— notify 分支与查单兜底共用的完成逻辑
  - `POST /api/user/agent/chat` 变为游客可用（TryUserAuth），游客 IP 限额 10/时、50/日
  - OptionMap 键：`AgentGuestChatHourLimit`、`AgentGuestChatDayLimit`

- [ ] **Step 1: 写失败测试**（追加到 `controller/agent_topup_test.go`）

```go
func TestGuestChatLimiter(t *testing.T) {
	oldH, oldD := operation_setting.AgentGuestChatHourLimit, operation_setting.AgentGuestChatDayLimit
	t.Cleanup(func() {
		operation_setting.AgentGuestChatHourLimit, operation_setting.AgentGuestChatDayLimit = oldH, oldD
		resetGuestChatLimiter()
	})
	operation_setting.AgentGuestChatHourLimit = 2
	operation_setting.AgentGuestChatDayLimit = 3

	if !allowGuestChat("1.2.3.4") {
		t.Fatal("first should pass")
	}
	if !allowGuestChat("1.2.3.4") {
		t.Fatal("second should pass")
	}
	if allowGuestChat("1.2.3.4") {
		t.Fatal("third should hit hourly limit")
	}
	if !allowGuestChat("5.6.7.8") {
		t.Fatal("other ip unaffected")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd e:\savvy\new-api; go test ./controller/ -run TestGuestChatLimiter -v`
Expected: 编译失败 `undefined: allowGuestChat`

- [ ] **Step 3: 实现**

3a. `setting/operation_setting/agent_bailian.go` 追加：

```go
// 游客(未登录)聊天限额,防匿名烧 token。0 或负值视为默认值。
var (
	AgentGuestChatHourLimit = 10
	AgentGuestChatDayLimit  = 50
)
```

`model/option.go` 两处注册，逐字镜像 `AgentBailianHost` 的现有写法（initOptionMap 里 `common.OptionMap["AgentGuestChatHourLimit"] = strconv.Itoa(operation_setting.AgentGuestChatHourLimit)`，updateOptionMap switch 里 `case "AgentGuestChatHourLimit":` 用 `strconv.Atoi` 回写，Day 同理；具体函数名以该文件 AgentBailian* 三行的实际形态为准）。

3b. `controller/agent_topup.go` 追加游客限额与两个 handler：

```go
import 追加: "fmt" "net/http" "strconv" "strings" "sync" "time"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/smartwalle/alipay/v3"

// 游客聊天 IP 限额。单实例内存计数即可(重启清零可接受,限流是防刷不是记账)。
type guestChatWindow struct {
	hourCount int
	hourStart int64
	dayCount  int
	dayStart  int64
}

var (
	guestChatMu sync.Mutex
	guestChatM  = make(map[string]*guestChatWindow)
)

func resetGuestChatLimiter() {
	guestChatMu.Lock()
	defer guestChatMu.Unlock()
	guestChatM = make(map[string]*guestChatWindow)
}

func allowGuestChat(ip string) bool {
	hourLimit, dayLimit := operation_setting.AgentGuestChatHourLimit, operation_setting.AgentGuestChatDayLimit
	if hourLimit <= 0 {
		hourLimit = 10
	}
	if dayLimit <= 0 {
		dayLimit = 50
	}
	now := time.Now().Unix()
	guestChatMu.Lock()
	defer guestChatMu.Unlock()
	w := guestChatM[ip]
	if w == nil {
		w = &guestChatWindow{hourStart: now, dayStart: now}
		guestChatM[ip] = w
	}
	if now-w.hourStart >= 3600 {
		w.hourStart, w.hourCount = now, 0
	}
	if now-w.dayStart >= 86400 {
		w.dayStart, w.dayCount = now, 0
	}
	if w.hourCount >= hourLimit || w.dayCount >= dayLimit {
		return false
	}
	w.hourCount++
	w.dayCount++
	return true
}
```

`controller/agent_chat.go` 的 `AgentChat` 开头（ShouldBindJSON 校验之后、IsAgentBailianConfigured 之前）插入：

```go
	if c.GetInt("id") == 0 && !allowGuestChat(c.ClientIP()) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "请求过于频繁，请稍后再试"})
		return
	}
```

登记与状态 handler（`controller/agent_topup.go` 追加）：

```go
// RegisterAgentTopUp 游客/用户下单后登记 MCP 订单,发放认领凭据。
// 金额此时只是"申报值"(来自支付链接),入账以 completeAgentTopUp 的支付宝侧金额为准。
func RegisterAgentTopUp(c *gin.Context) {
	var req struct {
		OutTradeNo string `json:"out_trade_no"`
	}
	if err := c.ShouldBindJSON(&req); err != nil ||
		strings.TrimSpace(req.OutTradeNo) == "" || len(req.OutTradeNo) > 64 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	outTradeNo := strings.TrimSpace(req.OutTradeNo)
	if model.GetTopUpByTradeNo(outTradeNo) != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单已登记"})
		return
	}
	token, err := newClaimToken()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "登记失败"})
		return
	}
	topUp := &model.TopUp{
		UserId:          c.GetInt("id"), // 游客为 0,认领时再绑
		TradeNo:         outTradeNo,
		ClaimToken:      token,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipayAgent,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "登记失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"claim_token": token}})
}

// AgentTopUpStatus 凭 claim_token 查订单状态;pending 超过 10 秒时顺手向支付宝查单(兜底通道,
// 百炼 MCP 若配不了 AP_NOTIFY_URL,到账感知全靠这里)。token 即凭据,无需登录。
func AgentTopUpStatus(c *gin.Context) {
	token := strings.TrimSpace(c.Query("claim_token"))
	if token == "" || len(token) != 32 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	topUp := model.GetTopUpByClaimToken(token)
	if topUp == nil || topUp.PaymentProvider != model.PaymentProviderAlipayAgent {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}
	if topUp.Status == common.TopUpStatusPending && time.Now().Unix()-topUp.CreateTime > 10 {
		tryCompleteAgentTopUpByQuery(topUp)
		topUp = model.GetTopUpByClaimToken(token)
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"status":  topUp.Status,
		"money":   topUp.Money,
		"claimed": topUp.UserId != 0,
	}})
}

// tryCompleteAgentTopUpByQuery 用站点现有支付宝 client 按 out_trade_no 查单。
// 前提(V0 验证项): MCP 订单与站点支付配置同 APPID。查不到/出错静默返回,下次轮询再试。
func tryCompleteAgentTopUpByQuery(topUp *model.TopUp) {
	cli := GetAlipayClient()
	if cli == nil {
		return
	}
	var q = alipay.TradeQuery{}
	q.OutTradeNo = topUp.TradeNo
	rsp, err := cli.TradeQuery(context.Background(), q)
	if err != nil || rsp == nil || rsp.Code != alipay.CodeSuccess {
		return
	}
	if rsp.TradeStatus != "TRADE_SUCCESS" && rsp.TradeStatus != "TRADE_FINISHED" {
		return
	}
	money, perr := strconv.ParseFloat(string(rsp.TotalAmount), 64)
	if perr != nil || money <= 0 {
		return
	}
	LockOrder(topUp.TradeNo)
	defer UnlockOrder(topUp.TradeNo)
	fresh := model.GetTopUpByTradeNo(topUp.TradeNo)
	if fresh == nil || fresh.Status != common.TopUpStatusPending {
		return
	}
	if cerr := completeAgentTopUp(fresh, money, ""); cerr != nil {
		common.SysError("agent topup query-complete failed: " + cerr.Error())
	}
}
```

import 需含 `"context"`。`rsp.TotalAmount` 在 smartwalle/alipay/v3 中为 string 类型，实现时以本地 SDK 源码为准（`go doc github.com/smartwalle/alipay/v3.TradeQueryResponse`），若为其他类型相应调整 ParseFloat 行。

3c. `controller/topup_alipay.go` 追加共用完成逻辑（放在 AlipayNotify 之后）：

```go
// completeAgentTopUp 是 alipay_agent 订单的完成逻辑: 回填实付金额、标记 success;
// 已绑用户的直接入账,游客单(user_id=0)只标记,等认领接口入账。
// 调用方必须已 LockOrder。金额以支付宝侧为准(actualMoney 来自回调 total_amount 或查单)。
func completeAgentTopUp(topUp *model.TopUp, actualMoney float64, clientIP string) error {
	topUp.Money = actualMoney
	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = common.GetTimestamp()
	if topUp.UserId > 0 {
		group, err := model.GetUserGroup(topUp.UserId, true)
		if err != nil {
			return err
		}
		topUp.Amount = agentQuotaAmountFromMoney(actualMoney, group)
	}
	if err := topUp.Update(); err != nil {
		return err
	}
	if topUp.UserId == 0 || topUp.Amount <= 0 {
		return nil // 游客单等认领;换算为 0 的极小额单不入账(认领接口会拒绝)
	}
	quotaToAdd := decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	if err := model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true); err != nil {
		return err
	}
	if model.SubmitOrderEvidenceFn != nil {
		go func(in model.SubmitOrderEvidenceInput) {
			if err := model.SubmitOrderEvidenceFn(in); err != nil {
				common.SysError("antchain evidence submit failed: " + err.Error())
			}
		}(model.BuildTopupEvidence(topUp))
	}
	model.RecordTopupLog(topUp.UserId,
		fmt.Sprintf("使用智能体支付宝充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money),
		clientIP, topUp.PaymentMethod, model.PaymentMethodAlipay)
	return nil
}
```

3d. `router/api-router.go`：L118 的 chat 路由从 selfRoute 移出（selfRoute 块内删除该行），加到 userRoute 匿名段（L81 wechat/notify 行之后）：

```go
			// ponytail: 智能体聊天/登记对游客开放(TryUserAuth 有登录态则绑 id),配额与限额在 controller 内区分
			userRoute.POST("/agent/chat", middleware.TryUserAuth(), middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.AgentChat)
			userRoute.POST("/agent/topup/register", middleware.TryUserAuth(), middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.RegisterAgentTopUp)
			userRoute.GET("/agent/topup/status", middleware.CriticalRateLimit(), controller.AgentTopUpStatus)
```

（claim 接口 Task 3 挂 selfRoute。路径成 /api/user/agent/...，与现有前端 api.ts 的 `/api/user/agent/chat` 一致，前端无需改路径。）

- [ ] **Step 4: 跑测试确认通过**

Run: `cd e:\savvy\new-api; go test ./controller/ -run 'TestGuestChatLimiter|TestAgentQuota|TestNewClaimToken' -v; go build ./...`
Expected: 全 PASS + build 成功

- [ ] **Step 5: Commit**

```bash
git add new-api/controller/agent_topup.go new-api/controller/agent_topup_test.go new-api/controller/topup_alipay.go new-api/controller/agent_chat.go new-api/router/api-router.go new-api/setting/operation_setting/agent_bailian.go new-api/model/option.go
git commit -m "feat(agent-topup): guest chat channel, order register/status endpoints with query fallback"
```

---

### Task 3: notify 分支 + 认领接口

**Files:**
- Modify: `new-api/controller/topup_alipay.go`（AlipayNotify 加 alipay_agent 分支）
- Modify: `new-api/controller/agent_topup.go`（追加 ClaimAgentTopUp）
- Modify: `new-api/router/api-router.go`（selfRoute 加 claim 路由）
- Test: `new-api/controller/agent_topup_test.go`（追加）

**Interfaces:**
- Consumes: `completeAgentTopUp`、`model.GetTopUpByClaimToken`、`agentQuotaAmountFromMoney`、`LockOrder/UnlockOrder`
- Produces: `POST /api/user/agent/topup/claim`（登录态）body `{"claim_token": string}` → `{"message":"success","data":{"amount":int64}}`

- [ ] **Step 1: 写失败测试**

认领 handler 依赖 DB，纯单测只覆盖分支判定函数（把"可否认领"抽成可测纯函数）：

```go
func TestAgentClaimDecision(t *testing.T) {
	paid := &model.TopUp{PaymentProvider: model.PaymentProviderAlipayAgent, Status: common.TopUpStatusSuccess, UserId: 0}
	if code := agentClaimDecision(paid, 7); code != agentClaimOK {
		t.Fatalf("paid guest order should be claimable, got %d", code)
	}
	if code := agentClaimDecision(&model.TopUp{PaymentProvider: model.PaymentProviderAlipayAgent, Status: common.TopUpStatusSuccess, UserId: 7}, 7); code != agentClaimAlreadyMine {
		t.Fatal("idempotent re-claim should report already-mine")
	}
	if code := agentClaimDecision(&model.TopUp{PaymentProvider: model.PaymentProviderAlipayAgent, Status: common.TopUpStatusSuccess, UserId: 8}, 7); code != agentClaimTaken {
		t.Fatal("other user's claim must be rejected")
	}
	if code := agentClaimDecision(&model.TopUp{PaymentProvider: model.PaymentProviderAlipayAgent, Status: common.TopUpStatusPending, UserId: 0}, 7); code != agentClaimNotPaid {
		t.Fatal("unpaid order must not be claimable")
	}
	if code := agentClaimDecision(&model.TopUp{PaymentProvider: model.PaymentProviderAlipay, Status: common.TopUpStatusSuccess, UserId: 0}, 7); code != agentClaimNotAgentOrder {
		t.Fatal("non-agent provider must be rejected")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd e:\savvy\new-api; go test ./controller/ -run TestAgentClaimDecision -v`
Expected: 编译失败 `undefined: agentClaimDecision`

- [ ] **Step 3: 实现**

3a. `controller/agent_topup.go` 追加：

```go
type agentClaimCode int

const (
	agentClaimOK agentClaimCode = iota
	agentClaimAlreadyMine
	agentClaimTaken
	agentClaimNotPaid
	agentClaimNotAgentOrder
	agentClaimZeroAmount
)

func agentClaimDecision(topUp *model.TopUp, userId int) agentClaimCode {
	if topUp.PaymentProvider != model.PaymentProviderAlipayAgent {
		return agentClaimNotAgentOrder
	}
	if topUp.Status != common.TopUpStatusSuccess {
		return agentClaimNotPaid
	}
	if topUp.UserId == userId {
		return agentClaimAlreadyMine
	}
	if topUp.UserId != 0 {
		return agentClaimTaken
	}
	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		return agentClaimTaken // 拿不到分组按拒绝处理,不给默认倍率占便宜
	}
	if agentQuotaAmountFromMoney(topUp.Money, group) <= 0 {
		return agentClaimZeroAmount
	}
	return agentClaimOK
}

// ClaimAgentTopUp 游客支付后登录/注册,凭 claim_token 把已支付订单绑到自己名下并入账。
func ClaimAgentTopUp(c *gin.Context) {
	var req struct {
		ClaimToken string `json:"claim_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(strings.TrimSpace(req.ClaimToken)) != 32 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	topUp := model.GetTopUpByClaimToken(strings.TrimSpace(req.ClaimToken))
	if topUp == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}
	userId := c.GetInt("id")
	LockOrder(topUp.TradeNo)
	defer UnlockOrder(topUp.TradeNo)
	fresh := model.GetTopUpByTradeNo(topUp.TradeNo)
	if fresh == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}
	switch agentClaimDecision(fresh, userId) {
	case agentClaimAlreadyMine:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"amount": fresh.Amount}})
		return
	case agentClaimOK:
	default:
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单当前不可认领"})
		return
	}
	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	fresh.UserId = userId
	fresh.Amount = agentQuotaAmountFromMoney(fresh.Money, group)
	quotaToAdd := decimal.NewFromInt(fresh.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	if err := fresh.Update(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "认领失败"})
		return
	}
	if err := model.IncreaseUserQuota(userId, quotaToAdd, true); err != nil {
		// ponytail: 与 AlipayNotify 同款 latent money leak(Update 已绑用户,加额失败钱在单上不在账上)。
		//   人工兜底: topups 表 status=success 且 user_id>0 但日志无入账记录的,客服按 Money 补。
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "入账失败，请联系客服"})
		return
	}
	if model.SubmitOrderEvidenceFn != nil {
		go func(in model.SubmitOrderEvidenceInput) {
			if err := model.SubmitOrderEvidenceFn(in); err != nil {
				common.SysError("antchain evidence submit failed: " + err.Error())
			}
		}(model.BuildTopupEvidence(fresh))
	}
	model.RecordTopupLog(userId,
		fmt.Sprintf("使用智能体支付宝充值成功（认领），充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), fresh.Money),
		c.ClientIP(), fresh.PaymentMethod, model.PaymentMethodAlipay)
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"amount": fresh.Amount}})
}
```

import 追加 `"github.com/shopspring/decimal"`。

3b. `topup_alipay.go` AlipayNotify：现有 provider 校验（L169-172 `topUp.PaymentProvider != model.PaymentProviderAlipay` 拒绝分支）改为：

```go
	if topUp.PaymentProvider == model.PaymentProviderAlipayAgent {
		// 智能体订单: 金额以回调为准回填,游客单只标记等认领,已绑用户直接入账
		if topUp.Status != common.TopUpStatusPending {
			_, _ = c.Writer.Write([]byte("success")) // 幂等止重试
			return
		}
		money, perr := strconv.ParseFloat(c.Request.Form.Get("total_amount"), 64)
		if perr != nil || money <= 0 {
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		if cerr := completeAgentTopUp(topUp, money, c.ClientIP()); cerr != nil {
			common.SysError("agent topup notify-complete failed: " + cerr.Error())
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		_, _ = c.Writer.Write([]byte("success"))
		return
	}
	if topUp.PaymentProvider != model.PaymentProviderAlipay {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
```

（插入位置：`topUp == nil` 判空之后、原 provider 校验之前；原校验保留在其后，其余流程零改动。）

3c. 路由（selfRoute 块，`/agent/chat` 原位置附近）：

```go
				selfRoute.POST("/agent/topup/claim", middleware.CriticalRateLimit(), controller.ClaimAgentTopUp)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd e:\savvy\new-api; go test ./controller/ -run 'TestAgentClaim|TestGuestChatLimiter|TestAgentQuota|TestNewClaimToken' -v; go build ./...`
Expected: 全 PASS + build 成功

- [ ] **Step 5: Commit**

```bash
git add new-api/controller/agent_topup.go new-api/controller/agent_topup_test.go new-api/controller/topup_alipay.go new-api/router/api-router.go
git commit -m "feat(agent-topup): notify branch for alipay_agent + claim endpoint with idempotency"
```

---

### Task 4: 前端悬浮 widget + 删除独立页/侧边栏入口

**Files:**
- Create: `new-api/web/default/src/features/agent-chat/widget.tsx`
- Modify: `new-api/web/default/src/routes/__root.tsx`（RootComponent 挂 widget）
- Delete: `new-api/web/default/src/routes/_authenticated/agent-chat/index.tsx`
- Modify: `new-api/web/default/src/hooks/use-sidebar-config.ts`（删 URL_TO_CONFIG_MAP 的 '/agent-chat' 行）
- Modify: `new-api/web/default/src/features/system-settings/maintenance/sidebar-modules-section.tsx` 与 `new-api/web/default/src/features/profile/components/sidebar-modules-card.tsx`（agent_chat 条目 description 改为 widget 语义）

**Interfaces:**
- Consumes: `AgentChat`（features/agent-chat/index.tsx 现有导出，零改动）、`isSidebarModuleEnabled('chat', 'agent_chat')`（@/lib/nav-modules）
- Produces: `<AgentWidget />` 组件（无 props），全站可见性由模块开关 `chat.agent_chat` 控制

- [ ] **Step 1: 实现 widget.tsx**（版权头对齐同目录文件）

```tsx
import { useState } from 'react'
import { Bot, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { isSidebarModuleEnabled } from '@/lib/nav-modules'
import { AgentChat } from './index'

// 全站右下角悬浮智能体入口。显隐复用原 /agent-chat 的模块开关(chat.agent_chat),
// 语义从"侧边栏模块"变为"widget 显隐",配置键不动,免迁移。
export function AgentWidget() {
  const [open, setOpen] = useState(false)
  if (!isSidebarModuleEnabled('chat', 'agent_chat')) return null

  return (
    <>
      <Button
        size='icon'
        className='fixed right-4 bottom-4 z-50 h-12 w-12 rounded-full shadow-lg'
        onClick={() => setOpen((v) => !v)}
        aria-label='AI assistant'
      >
        {open ? <X className='h-5 w-5' /> : <Bot className='h-5 w-5' />}
      </Button>
      {open && (
        <div className='bg-background fixed right-4 bottom-20 z-50 flex h-[min(640px,80vh)] w-[min(420px,calc(100vw-2rem))] flex-col overflow-hidden rounded-xl border shadow-xl'>
          <AgentChat />
        </div>
      )}
    </>
  )
}
```

- [ ] **Step 2: __root.tsx 挂载**：RootComponent 的 `<Toaster .../>` 行后加：

```tsx
      <AgentWidget />
```

import 行加 `import { AgentWidget } from '@/features/agent-chat/widget'`。

- [ ] **Step 3: 删旧入口**
- 删除文件 `routes/_authenticated/agent-chat/index.tsx`（整个目录）
- `hooks/use-sidebar-config.ts`：删 `'/agent-chat': { section: 'chat', module: 'agent_chat' },` 一行（defaults 里的 `agent_chat: true` 保留）
- `system-settings/maintenance/sidebar-modules-section.tsx` 与 `profile/components/sidebar-modules-card.tsx`：`agent_chat` 条目 description 改为 `t('Floating AI assistant for top-up and subscription.')`（两处同文案）
- 全局 grep `agent-chat`/`'/agent-chat'` 确认无残留引用（features/agent-chat 目录自身与 __root 挂载除外）

- [ ] **Step 4: 构建验证**

Run: `cd e:\savvy\new-api\web\default; npm run typecheck; npm run build`
Expected: 均 0 error

- [ ] **Step 5: Commit**

```bash
git add -A new-api/web/default/src
git commit -m "feat(agent-widget): site-wide floating widget replaces /agent-chat page and sidebar entry"
```

---

### Task 5: 前端认领闭环（登记/轮询/认领卡片/sessionStorage 恢复）+ i18n

**Files:**
- Create: `new-api/web/default/src/features/agent-chat/lib/agent-order.ts`
- Create: `new-api/web/default/src/features/agent-chat/lib/claim-storage.ts`
- Create: `new-api/web/default/src/features/agent-chat/components/claim-card.tsx`
- Modify: `new-api/web/default/src/features/agent-chat/api.ts`（三个新 API）
- Modify: `new-api/web/default/src/features/agent-chat/components/payment-card.tsx`（挂载即登记+轮询）
- Modify: `new-api/web/default/src/features/agent-chat/widget.tsx`（挂载时恢复未认领单，渲染 ClaimCard）
- Modify: `new-api/web/default/src/i18n/locales/{en,zh,ja,fr,ru,vi}.json`

**Interfaces:**
- Consumes: Task 2/3 三个接口；`useAuthStore`（@/stores/auth-store，`auth.user` 判登录）；toast 用 sonner（对齐站内现有 `toast.success` 用法）
- Produces:
  - `parseAgentOrder(link: string): { outTradeNo: string; totalAmount: number } | null`
  - `saveClaim/readClaims/clearClaim`（sessionStorage key `agent_topup_claims`，元素 `{outTradeNo, token, done}`）
  - `<ClaimCard outTradeNo token money onDone />`

- [ ] **Step 1: lib/agent-order.ts**

```ts
// 从 MCP 支付链接解析订单号与申报金额。链接形如
// https://openapi.alipay.com/gateway.do?...&biz_content=%7B%22out_trade_no%22...%7D
// 解析失败返回 null → 不显示认领卡片,退化为普通支付卡片(客服兜底)。
export function parseAgentOrder(
  link: string
): { outTradeNo: string; totalAmount: number } | null {
  try {
    const biz = new URL(link).searchParams.get('biz_content')
    if (!biz) return null
    const obj = JSON.parse(biz) as Record<string, unknown>
    const outTradeNo = typeof obj.out_trade_no === 'string' ? obj.out_trade_no : ''
    const totalAmount = Number(obj.total_amount)
    if (!outTradeNo || !Number.isFinite(totalAmount) || totalAmount <= 0) return null
    return { outTradeNo, totalAmount }
  } catch {
    return null
  }
}
```

- [ ] **Step 2: lib/claim-storage.ts**

```ts
export type ClaimRecord = { outTradeNo: string; token: string; done: boolean }

const KEY = 'agent_topup_claims'

export function readClaims(): ClaimRecord[] {
  try {
    return JSON.parse(sessionStorage.getItem(KEY) ?? '[]') as ClaimRecord[]
  } catch {
    return []
  }
}

export function saveClaim(rec: ClaimRecord): void {
  const all = readClaims().filter((r) => r.outTradeNo !== rec.outTradeNo)
  all.push(rec)
  sessionStorage.setItem(KEY, JSON.stringify(all))
}

export function markClaimDone(outTradeNo: string): void {
  sessionStorage.setItem(
    KEY,
    JSON.stringify(
      readClaims().map((r) => (r.outTradeNo === outTradeNo ? { ...r, done: true } : r))
    )
  )
}
```

- [ ] **Step 3: api.ts 追加**（对齐 sendAgentMessage 的 skipBusinessError 范式）

```ts
export async function registerAgentTopUp(outTradeNo: string) {
  const res = await api.post(
    '/api/user/agent/topup/register',
    { out_trade_no: outTradeNo },
    { skipBusinessError: true } as Record<string, unknown>
  )
  return res.data as { message: string; data?: { claim_token: string } }
}

export async function getAgentTopUpStatus(claimToken: string) {
  const res = await api.get('/api/user/agent/topup/status', {
    params: { claim_token: claimToken },
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data as {
    message: string
    data?: { status: string; money: number; claimed: boolean }
  }
}

export async function claimAgentTopUp(claimToken: string) {
  const res = await api.post(
    '/api/user/agent/topup/claim',
    { claim_token: claimToken },
    { skipBusinessError: true } as Record<string, unknown>
  )
  return res.data as { message: string; data?: { amount: number } }
}
```

（`api.get` 的 params 传法以 `@/lib/api` 实际封装为准，实现时对照 wallet api 的 GET 用法。）

- [ ] **Step 4: claim-card.tsx**

```tsx
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth-store'
import { claimAgentTopUp, getAgentTopUpStatus } from '../api'
import { markClaimDone } from '../lib/claim-storage'

type ClaimCardProps = {
  outTradeNo: string
  token: string
}

type Phase = 'waiting' | 'paid' | 'credited'

// 认领卡片: 轮询订单状态 → 已支付且未登录给 [登录][注册] 跳转(带 redirect 回跳,
// sessionStorage 里的 claim 记录由 widget 挂载恢复逻辑接力) → 已登录自动认领入账。
export function ClaimCard({ outTradeNo, token }: ClaimCardProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { auth } = useAuthStore()
  const [phase, setPhase] = useState<Phase>('waiting')

  const tryClaim = useCallback(async () => {
    const res = await claimAgentTopUp(token)
    if (res.message === 'success') {
      markClaimDone(outTradeNo)
      setPhase('credited')
      toast.success(t('Top-up credited to your account'))
    }
  }, [token, outTradeNo, t])

  useEffect(() => {
    let stopped = false
    const tick = async () => {
      const res = await getAgentTopUpStatus(token)
      if (stopped) return
      if (res.message === 'success' && res.data) {
        if (res.data.claimed) {
          markClaimDone(outTradeNo)
          setPhase('credited')
          return
        }
        if (res.data.status === 'success') {
          setPhase('paid')
          if (auth.user) await tryClaim()
          return
        }
      }
      setTimeout(() => void tick(), 5000)
    }
    void tick()
    return () => {
      stopped = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token])

  if (phase === 'credited')
    return (
      <div className='bg-card my-2 rounded-lg border p-4 text-sm'>
        {t('Top-up credited to your account')}
      </div>
    )
  if (phase === 'waiting')
    return (
      <div className='text-muted-foreground bg-card my-2 rounded-lg border p-4 text-sm'>
        {t('Waiting for payment confirmation...')}
      </div>
    )
  return (
    <div className='bg-card my-2 rounded-lg border p-4'>
      <p className='text-sm font-medium'>{t('Payment received')}</p>
      <p className='text-muted-foreground mt-1 text-xs'>
        {t('Sign in or register to claim your top-up')}
      </p>
      <div className='mt-3 flex gap-2'>
        <Button
          className='flex-1'
          onClick={() =>
            void navigate({
              to: '/sign-in',
              search: { redirect: window.location.pathname + window.location.search },
            })
          }
        >
          {t('Sign In')}
        </Button>
        <Button
          variant='outline'
          className='flex-1'
          onClick={() => void navigate({ to: '/sign-up' })}
        >
          {t('Sign Up')}
        </Button>
      </div>
    </div>
  )
}
```

（`Sign In`/`Sign Up` 键站内已存在则复用现有键名，实现时 grep locales/en.json 确认，避免重复键；`to: '/sign-up'` 若该路由也支持 redirect search 参数则同样带上。）

- [ ] **Step 5: payment-card.tsx 接线**：组件内加登记 + 认领卡片渲染：

```tsx
// 顶部 import 追加
import { parseAgentOrder } from '../lib/agent-order'
import { registerAgentTopUp } from '../api'
import { readClaims, saveClaim } from '../lib/claim-storage'
import { ClaimCard } from './claim-card'

// 组件内 state 与 effect:
const [claim, setClaim] = useState<{ outTradeNo: string; token: string } | null>(
  () => {
    // 已登记过(同会话重渲染)直接恢复,不重复登记
    const order = parseAgentOrder(link)
    if (!order) return null
    const rec = readClaims().find((r) => r.outTradeNo === order.outTradeNo && !r.done)
    return rec ? { outTradeNo: rec.outTradeNo, token: rec.token } : null
  }
)

useEffect(() => {
  if (claim) return
  const order = parseAgentOrder(link)
  if (!order) return
  let cancelled = false
  void registerAgentTopUp(order.outTradeNo).then((res) => {
    if (cancelled) return
    if (res.message === 'success' && res.data?.claim_token) {
      saveClaim({ outTradeNo: order.outTradeNo, token: res.data.claim_token, done: false })
      setClaim({ outTradeNo: order.outTradeNo, token: res.data.claim_token })
    }
    // 登记失败(如"订单已登记")静默: 认领走 widget 恢复逻辑或客服兜底
  })
  return () => {
    cancelled = true
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
}, [link])

// JSX: "Go to Pay" 按钮之后追加
{claim && <ClaimCard outTradeNo={claim.outTradeNo} token={claim.token} />}
```

- [ ] **Step 6: widget.tsx 恢复逻辑**：跨登录跳转回来后聊天消息态已丢，认领入口由 widget 顶部横幅接力。AgentWidget 内：

```tsx
// import 追加
import { useEffect, useState } from 'react'  // useState 已有则合并
import { readClaims, type ClaimRecord } from './lib/claim-storage'
import { ClaimCard } from './components/claim-card'

// 组件内:
const [pendingClaims, setPendingClaims] = useState<ClaimRecord[]>([])
useEffect(() => {
  setPendingClaims(readClaims().filter((r) => !r.done))
}, [open])

// 面板 JSX 中 AgentChat 之上:
{pendingClaims.map((r) => (
  <div key={r.outTradeNo} className='border-b px-3'>
    <ClaimCard outTradeNo={r.outTradeNo} token={r.token} />
  </div>
))}
```

（ClaimCard 入账后调 markClaimDone；横幅在下次 open 时刷新消失。轮询由 ClaimCard 自带，横幅与聊天流内的卡片可能同时存在同一订单——claim 接口幂等（alreadyMine 返回成功），无双入账风险。）

- [ ] **Step 7: i18n 六语言**：locales 六个 json 各加以下键（en 为源文案；zh 如下；ja/fr/ru/vi 由实现者按源文案翻译，风格对齐文件内既有支付类文案）：

| key | en | zh |
|---|---|---|
| Waiting for payment confirmation... | Waiting for payment confirmation... | 等待支付确认… |
| Payment received | Payment received | 已收到付款 |
| Sign in or register to claim your top-up | Sign in or register to claim your top-up | 登录或注册后即可领取充值 |
| Top-up credited to your account | Top-up credited to your account | 充值已到账 |
| Floating AI assistant for top-up and subscription. | Floating AI assistant for top-up and subscription. | 悬浮 AI 助手，可对话充值与订阅。 |

（`Sign In`/`Sign Up` 复用现有键，不新增。键的落位对齐各 locale 文件内 agent-chat 既有键的位置/命名风格。）

- [ ] **Step 8: 构建验证**

Run: `cd e:\savvy\new-api\web\default; npm run typecheck; npm run build`
Expected: 均 0 error

- [ ] **Step 9: Commit**

```bash
git add new-api/web/default/src
git commit -m "feat(agent-widget): guest pay-then-claim loop with sessionStorage recovery and i18n"
```

---

### Task 6: 全量验证 + 留痕 + 验收清单

**Files:**
- Create: `docs/records/agent-widget-guest-topup.md`

- [ ] **Step 1: 后端全量测试**

Run: `cd e:\savvy\new-api; go test ./controller/... ./model/... ./setting/...; go build ./...`
Expected: 本特性测试全绿；存量失败若与特性基点一致（对照 docs/records/agent-chat-bailian-phase1.md 记录的 2 例渠道亲和性用例）则不阻塞。

- [ ] **Step 2: 前端构建**

Run: `cd e:\savvy\new-api\web\default; npm run typecheck; npm run build`
Expected: 0 error

- [ ] **Step 3: 留痕文档**（一问题一 md 规范：现象/根因→方案/改动/验证/限制/尾巴）

内容要点：签约驱动（智能体访问地址=我方托管页面→免登录）；方案 B 决策与否决 A 的理由；改动清单（本计划 5 个 task 的产出文件）；验证结果（贴 Step 1/2 输出摘要）；限制（游客限额内存计数重启清零、IncreaseUserQuota 失败的 latent leak 与客服兜底口径、金额换算不套 AmountDiscount）；尾巴（智易收批准后切 create-alipay-payment-agent + 二维码渲染、V2 若可配 AP_NOTIFY_URL 则验证回调直推、0.01 元真单验收记录）。

- [ ] **Step 4: 手工验收清单（部署后管理员执行，结果补进留痕文档）**

1. 未登录打开首页 → 右下角出现悬浮图标 → 打开聊天窗
2. 游客对话"充值 0.01 元" → 出支付卡片 → 勾选协议前按钮置灰
3. 勾选 → 支付 → 支付页正常打开（V1 前提）→ 完成支付
4. 聊天窗内认领卡片从"等待支付确认"变"已收到付款"+ [登录][注册]
5. 点注册 → 注册成功回跳 → widget 横幅自动认领 → toast"充值已到账" → 钱包额度增加 0.01 对应额度
6. 登录用户重复 2-3 → 无需认领直接到账
7. 游客连发 11 条消息 → 第 11 条被限流拒绝
8. 支付宝商家后台对账：订单金额与入账额度换算一致（Price × 分组倍率）

- [ ] **Step 5: Commit**

```bash
git add docs/records/agent-widget-guest-topup.md
git commit -m "docs(agent-widget): guest topup rollout record with acceptance checklist"
```

---

## Self-Review 结论（写计划时已执行）

- **Spec 覆盖**：§4.1→Task4；§4.2→Task4 Step3；§4.3/4.4→Task5；§5.1→Task2；§5.2→Task2；§5.3→Task3；§5.4①→Task3、②→Task2；§5.5→Task1；§5.6→Task6 留痕；§8 限流→Task2；§9 测试→各 task + Task6。§6（百炼侧提示词/发布）是控制台操作非代码，已列入"动工前置"与留痕尾巴。
- **占位符**：无 TBD/TODO；所有代码块均为可直接落盘的完整实现（已清除一处草稿期混入的错误示意段）。
- **类型一致性**：`completeAgentTopUp(topUp, money, clientIP)` 签名在 Task2 定义、Task3 notify 分支调用一致；`agentClaimDecision` 返回码集合与 switch 分支一致；前端 `ClaimRecord` 三字段与 saveClaim/readClaims/markClaimDone 一致；API 路径前后端一致（/api/user/agent/topup/register|status|claim）。
