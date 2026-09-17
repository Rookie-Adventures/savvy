# 充值审计记录补齐 + 微信注册开关 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让支付宝/微信充值订单历史具备审计级证据链（渠道交易号、付款人、到账账户、前后余额、时间线），并新增"微信注册"独立开关（预留支付宝注册占位）。

**Architecture:** `topups` 表新增 8 列（AutoMigrate 自动迁移）；新增 `model.CompleteTopUpWithAudit` 单事务入账（FOR UPDATE 读余额→改订单→加额度），替换回调中"先改单再异步加额度"的两步式；前端订单卡片新增审计明细区。注册开关沿用 OptionMap 模式。

**Tech Stack:** Go (Gin + GORM, MySQL/PG/SQLite)、wechatpay-go v0.2.21、smartwalle/alipay v3、React + TSX (web/default)、i18next。

**Spec:** `docs/superpowers/specs/2026-09-17-topup-audit-records-design.md`

## Global Constraints

- 分支：`feat/topup-audit-records`，禁止直接在 dev 上改。
- Go 禁用 `encoding/json` 直调，一律 `common.Marshal/Unmarshal/GetJsonString`（`new-api/common/json.go`）。例外：`controller/wechat.go` 现存 `json.NewDecoder` 不动、不扩散。
- 新建 TSX 文件必须带 QuantumNous AGPL 头（照抄 `billing-history-dialog.tsx` L1-18）。
- GORM 不用 boolean default tag（`new-api/AGENTS.md` L88）。
- i18n 本任务只补 `locales/en.json` 与 `locales/zh.json`。
- 海外渠道（stripe/creem/waffo/epay）入账逻辑不动；蚂蚁链存证代码（`SubmitOrderEvidenceFn`）保留不动、不展示。
- 后台设置 UI 仅改 default 主题，classic 不动。
- 所有 Go 命令在 `e:\savvy\new-api` 下执行；前端命令在 `e:\savvy\new-api\web\default` 下执行。

---

### Task 1: model 层 — TopUp 新增审计字段 + CompleteTopUpWithAudit

**Files:**
- Modify: `new-api/model/topup.go`（TopUp 结构体 L14-27 + 文件末尾新增函数）
- Test: `new-api/model/topup_audit_test.go`（新建）

**Interfaces:**
- Produces: `type TopUpAudit struct { ChannelTradeNo string; PayerId string; PayerAccount string; ChannelPayTime int64 }`；`func CompleteTopUpWithAudit(tradeNo string, expectedProvider string, audit TopUpAudit, mutate func(tu *TopUp)) error`（Task 2/3 依赖）。TopUp 结构体新增 JSON 字段（Task 5 前端依赖）。

- [ ] **Step 1: 写失败测试**

`new-api/model/topup_audit_test.go`（model 包已有 TestMain 初始化 `DB`，参照 `model/wechat_identity_test.go` 直接用 `DB.AutoMigrate`）：

```go
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTopUpAuditTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &User{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM topups")
		DB.Exec("DELETE FROM users")
	})
}

func newAuditUser(t *testing.T, quota int64) *User {
	t.Helper()
	u := &User{Username: "u" + common.GetRandomString(6), Status: common.UserStatusEnabled, Quota: quota}
	require.NoError(t, u.Insert(0))
	return u
}

func newPendingTopUp(t *testing.T, userId int, tradeNo string) *TopUp {
	t.Helper()
	tu := &TopUp{UserId: userId, Amount: 10, Money: 9.9, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodAlipay, PaymentProvider: PaymentProviderAlipay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending}
	require.NoError(t, tu.Insert())
	return tu
}

func TestCompleteTopUpWithAudit_Success(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 52000000)
	tu := newPendingTopUp(t, u.Id, "AUDIT-1")

	err := CompleteTopUpWithAudit("AUDIT-1", PaymentProviderAlipay,
		TopUpAudit{ChannelTradeNo: "2026091722001", PayerId: "2088x", ChannelPayTime: 1758090640}, nil)
	require.NoError(t, err)

	got := GetTopUpByTradeNo("AUDIT-1")
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Equal(t, "2026091722001", got.ChannelTradeNo)
	assert.Equal(t, "2088x", got.PayerId)
	assert.Equal(t, int64(52000000), got.BalanceBefore)
	assert.Equal(t, int64(52000000+10*int64(common.QuotaPerUnit)), got.BalanceAfter)
	assert.Equal(t, u.Username, got.CreditedUsername)
	assert.Greater(t, got.CompleteTime, int64(0))
	var after User
	require.NoError(t, DB.Where("id = ?", u.Id).First(&after).Error)
	assert.Equal(t, int64(52000000+10*int64(common.QuotaPerUnit)), after.Quota)
}

func TestCompleteTopUpWithAudit_Idempotent(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 0)
	newPendingTopUp(t, u.Id, "AUDIT-2")
	require.NoError(t, CompleteTopUpWithAudit("AUDIT-2", PaymentProviderAlipay, TopUpAudit{}, nil))
	require.NoError(t, CompleteTopUpWithAudit("AUDIT-2", PaymentProviderAlipay, TopUpAudit{}, nil))
	var after User
	require.NoError(t, DB.Where("id = ?", u.Id).First(&after).Error)
	assert.Equal(t, int64(10*int64(common.QuotaPerUnit)), after.Quota, "二次调用不得重复加钱")
}

func TestCompleteTopUpWithAudit_ProviderMismatch(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 0)
	newPendingTopUp(t, u.Id, "AUDIT-3")
	err := CompleteTopUpWithAudit("AUDIT-3", PaymentProviderWechat, TopUpAudit{}, nil)
	assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
}

func TestCompleteTopUpWithAudit_NotPending(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 0)
	tu := newPendingTopUp(t, u.Id, "AUDIT-4")
	tu.Status = common.TopUpStatusExpired
	require.NoError(t, tu.Update())
	assert.ErrorIs(t, CompleteTopUpWithAudit("AUDIT-4", PaymentProviderAlipay, TopUpAudit{}, nil), ErrTopUpStatusInvalid)
}

func TestCompleteTopUpWithAudit_MutateAndGuestOrder(t *testing.T) {
	setupTopUpAuditTest(t)
	// 游客单 user_id=0:只标记成功,不加余额
	tu := &TopUp{UserId: 0, Amount: 10, Money: 9.9, TradeNo: "AUDIT-5",
		PaymentMethod: PaymentMethodAlipay, PaymentProvider: PaymentProviderAlipayAgent,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending}
	require.NoError(t, tu.Insert())
	err := CompleteTopUpWithAudit("AUDIT-5", PaymentProviderAlipayAgent,
		TopUpAudit{ChannelTradeNo: "chan-1"}, func(tu *TopUp) { tu.Money = 9.9 })
	require.NoError(t, err)
	got := GetTopUpByTradeNo("AUDIT-5")
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Equal(t, "chan-1", got.ChannelTradeNo)
	assert.Equal(t, float64(9.9), got.Money, "mutate 回填生效")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./model/ -run TestCompleteTopUpWithAudit -v`
Expected: 编译失败（`CompleteTopUpWithAudit`/`TopUpAudit` 未定义）

- [ ] **Step 3: 最小实现**

`new-api/model/topup.go`：TopUp 结构体（L14-27）追加字段：

```go
	// 审计字段(支付宝/微信审核取证):渠道交易号/付款人/入账前后余额/到账账户快照/渠道支付时间
	ChannelTradeNo   string `json:"channel_trade_no" gorm:"type:varchar(64);index"`
	PayerId          string `json:"payer_id" gorm:"type:varchar(128)"`
	PayerAccount     string `json:"payer_account" gorm:"type:varchar(128)"`
	BalanceBefore    int64  `json:"balance_before"`
	BalanceAfter     int64  `json:"balance_after"`
	CreditedUsername string `json:"credited_username" gorm:"type:varchar(255)"`
	CreditedEmail    string `json:"credited_email" gorm:"type:varchar(255)"`
	ChannelPayTime   int64  `json:"channel_pay_time"`
```

文件末尾追加：

```go
// TopUpAudit 支付渠道侧的取证信息,随回调入账一次性落库。
type TopUpAudit struct {
	ChannelTradeNo string
	PayerId        string
	PayerAccount   string
	ChannelPayTime int64
}

// CompleteTopUpWithAudit 单事务完成充值单:FOR UPDATE 读单→校验→mutate 回填→快照余额→加额度。
// mutate 非 nil 时在行锁内回填额外字段(agent 单回填 Money/Amount 用);游客单(user_id=0)只标记不加余额。
// 幂等:已 success 直接返 nil。替换"先 Update 再异步 IncreaseUserQuota"的两步式,消除钱到账未加额度的隐患。
func CompleteTopUpWithAudit(tradeNo string, expectedProvider string, audit TopUpAudit, mutate func(tu *TopUp)) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if topUp.PaymentProvider != expectedProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if mutate != nil {
			mutate(topUp)
		}
		quotaToAdd := int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if topUp.UserId > 0 && quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		topUp.ChannelTradeNo = audit.ChannelTradeNo
		topUp.PayerId = audit.PayerId
		topUp.PayerAccount = audit.PayerAccount
		topUp.ChannelPayTime = audit.ChannelPayTime
		if topUp.UserId > 0 {
			user := &User{}
			if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", topUp.UserId).First(user).Error; err != nil {
				return err
			}
			topUp.BalanceBefore = user.Quota
			topUp.BalanceAfter = user.Quota + int64(quotaToAdd)
			topUp.CreditedUsername = user.Username
			topUp.CreditedEmail = user.Email
		}
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		if topUp.UserId > 0 {
			if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
```

注意：`User.Quota` 若为 `int` 而非 `int64`，以结构体实际类型为准统一（读 `model/user.go` 确认后对齐 BalanceBefore/After 类型）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./model/ -run TestCompleteTopUpWithAudit -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add new-api/model/topup.go new-api/model/topup_audit_test.go
git commit -m "feat(model): topup audit fields + transactional CompleteTopUpWithAudit"
```

---

### Task 2: 微信回调接入审计

**Files:**
- Modify: `new-api/controller/topup_wechat.go`（WechatNotify L169-208）
- Test: `new-api/controller/topup_wechat_audit_test.go`（新建）

**Interfaces:**
- Consumes: Task 1 `model.CompleteTopUpWithAudit(tradeNo, expectedProvider, audit, mutate)`、`model.TopUpAudit`。
- Produces: `type wxTopUpNotifyDetail struct`（payload 解析）。

- [ ] **Step 1: 写失败测试**

`new-api/controller/topup_wechat_audit_test.go`（复用同包测试 DB bootstrap，参照 `payment_safety_gates_test.go` 的 `setupModelListControllerTestDB(t)`）：

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWxTopUpNotifyDetail(t *testing.T) {
	payload := `{"out_trade_no":"WXUSR1NOX1","transaction_id":"4200002376202609173123456789","trade_state":"SUCCESS","success_time":"2026-09-17T14:30:40+08:00","payer":{"openid":"oX-8k5abc"}}`
	detail, err := parseWxTopUpNotifyDetail(payload)
	require.NoError(t, err)
	assert.Equal(t, "4200002376202609173123456789", detail.TransactionId)
	assert.Equal(t, "oX-8k5abc", detail.Payer.Openid)
	assert.Equal(t, int64(1758090640), detail.SuccessTimeUnix)
}

func TestParseWxTopUpNotifyDetail_MissingFields(t *testing.T) {
	detail, err := parseWxTopUpNotifyDetail(`{"out_trade_no":"X"}`)
	require.NoError(t, err, "缺字段不报错,只是空值/0")
	assert.Empty(t, detail.TransactionId)
	assert.Zero(t, detail.SuccessTimeUnix)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./controller/ -run TestParseWxTopUpNotifyDetail -v`
Expected: 编译失败（`parseWxTopUpNotifyDetail` 未定义）

- [ ] **Step 3: 实现**

`topup_wechat.go` 新增解析函数 + 重写 `WechatNotify` 的 finalize（保留 GetTopUpByTradeNo 早退幂等检查与 provider 校验，替换"Update + IncreaseUserQuota"为一次 `CompleteTopUpWithAudit`）：

```go
// wxTopUpNotifyDetail 微信充值回调明文中本路径消费的字段(其余忽略)。
type wxTopUpNotifyDetail struct {
	TransactionId string `json:"transaction_id"`
	SuccessTime   string `json:"success_time"` // RFC3339,解析失败置 0 不阻断
	Payer         struct {
		Openid string `json:"openid"`
	} `json:"payer"`
}

func parseWxTopUpNotifyDetail(plaintext string) (*wxTopUpNotifyDetail, error) {
	detail := &wxTopUpNotifyDetail{}
	if err := common.Unmarshal([]byte(plaintext), detail); err != nil {
		return nil, err
	}
	if detail.SuccessTime != "" {
		if ts, err := time.Parse(time.RFC3339, detail.SuccessTime); err == nil {
			detail.SuccessTimeUnix = ts.Unix()
		}
	}
	return detail, nil
}
```

（`wxTopUpNotifyDetail` 增加导出字段 `SuccessTimeUnix int64`；import 补 `"time"`，`encoding/json` 禁用——用 `common.Unmarshal`。）

`finalize` 中原 L189-201（topUp.Update / IncreaseUserQuota）替换为：

```go
		detail, perr := parseWxTopUpNotifyDetail(payload)
		if perr != nil {
			return fmt.Errorf("parse notify payload: %w", perr)
		}
		audit := model.TopUpAudit{
			ChannelTradeNo: detail.TransactionId,
			PayerId:        detail.Payer.Openid,
			ChannelPayTime: detail.SuccessTimeUnix,
		}
		if err := model.CompleteTopUpWithAudit(tradeNo, model.PaymentProviderWechat, audit, nil); err != nil {
			return err
		}
```

`RecordTopupLog` 调用保持原文案不变。`"encoding/json"` 若因此引入不要加（本文件原本没有）。

- [ ] **Step 4: 跑测试确认通过 + 回归**

Run: `go test ./controller/ -run TestParseWxTopUpNotifyDetail -v && go build ./...`
Expected: PASS + 编译通过

- [ ] **Step 5: 提交**

```bash
git add new-api/controller/topup_wechat.go new-api/controller/topup_wechat_audit_test.go
git commit -m "feat(wechat): capture transaction_id/payer/pay-time into topup audit"
```

---

### Task 3: 支付宝回调接入审计（普通单 + 智能体单）

**Files:**
- Modify: `new-api/controller/topup_alipay.go`（AlipayNotify L192-223、completeAgentTopUp L229-261）
- Test: `new-api/controller/topup_alipay_audit_test.go`（新建）

**Interfaces:**
- Consumes: Task 1 `model.CompleteTopUpWithAudit`、`model.TopUpAudit`。
- Produces: `func alipayAuditFromForm(form url.Values) (model.TopUpAudit, error)`、`completeAgentTopUp(topUp *model.TopUp, actualMoney float64, audit model.TopUpAudit, clientIP string) error`（签名变更，本文件内唯一调用点同步改）。

- [ ] **Step 1: 写失败测试**

`new-api/controller/topup_alipay_audit_test.go`：

```go
package controller

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlipayAuditFromForm(t *testing.T) {
	form := url.Values{}
	form.Set("trade_no", "2026091722001400001234567890")
	form.Set("buyer_id", "2088102177823456")
	form.Set("buyer_logon_id", "138****1234")
	form.Set("gmt_payment", "2026-09-17 14:30:40")
	audit, err := alipayAuditFromForm(form)
	require.NoError(t, err)
	assert.Equal(t, "2026091722001400001234567890", audit.ChannelTradeNo)
	assert.Equal(t, "2088102177823456", audit.PayerId)
	assert.Equal(t, "138****1234", audit.PayerAccount)
	assert.Equal(t, int64(1758090640), audit.ChannelPayTime)
}

func TestAlipayAuditFromForm_EmptyGmtPayment(t *testing.T) {
	form := url.Values{}
	form.Set("trade_no", "T1")
	audit, err := alipayAuditFromForm(form)
	require.NoError(t, err, "缺 gmt_payment 不报错")
	assert.Zero(t, audit.ChannelPayTime)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./controller/ -run TestAlipayAuditFromForm -v`
Expected: 编译失败

- [ ] **Step 3: 实现**

`topup_alipay.go`：

```go
// alipayAuditFromForm 从支付宝异步通知表单提取取证字段。
func alipayAuditFromForm(form url.Values) (model.TopUpAudit, error) {
	audit := model.TopUpAudit{
		ChannelTradeNo: form.Get("trade_no"),
		PayerId:        form.Get("buyer_id"),
		PayerAccount:   form.Get("buyer_logon_id"),
	}
	if gmt := form.Get("gmt_payment"); gmt != "" {
		ts, err := time.ParseInLocation("2006-01-02 15:04:05", gmt, time.Local)
		if err != nil {
			return audit, nil // 时间解析失败不阻断入账,置 0
		}
		audit.ChannelPayTime = ts.Unix()
	}
	return audit, nil
}
```

`AlipayNotify` 普通单路径（L197-219）：原 `topUp.Status/CompleteTime/Update + IncreaseUserQuota` 替换为：

```go
	audit, aerr := alipayAuditFromForm(c.Request.Form)
	if aerr != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := model.CompleteTopUpWithAudit(tradeNo, model.PaymentProviderAlipay, audit, nil); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	topUp = model.GetTopUpByTradeNo(tradeNo) // 刷新后供存证/log 使用
	quotaToAdd := int(decimal.NewFromInt(int64(topUp.Amount)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
```

存证 goroutine（L204-210）原样保留在 CompleteTopUpWithAudit 之后；`RecordTopupLog` 原样保留。

`completeAgentTopUp` 签名加 `audit model.TopUpAudit`：内部改为调用

```go
	if err := model.CompleteTopUpWithAudit(topUp.TradeNo, model.PaymentProviderAlipayAgent, audit, func(tu *model.TopUp) {
		tu.Money = actualMoney
	}); err != nil {
		return err
	}
```

原"Status/CompleteTime/Update"删除；`topUp.Amount = agentQuotaAmountFromMoney(...)` 逻辑保留并放进 mutate（Amount 回填需在事务内、额度计算前）；游客单分支（`user_id==0`）由 model 层自然处理。调用点（AlipayNotify L180）同步传 audit（先 `alipayAuditFromForm`，失败写 fail 早退）。

- [ ] **Step 4: 跑测试确认通过 + 回归**

Run: `go test ./controller/ -run "TestAlipay" -v && go build ./...`
Expected: PASS（含既有 TestAlipayNotify_RejectsOnVerifySignFailure 回归）

- [ ] **Step 5: 提交**

```bash
git add new-api/controller/topup_alipay.go new-api/controller/topup_alipay_audit_test.go
git commit -m "feat(alipay): capture trade_no/buyer/pay-time into topup audit (incl. agent orders)"
```

---

### Task 4: 管理员补单记录余额快照（内部，不加 UI）

**Files:**
- Modify: `new-api/model/topup.go`（ManualCompleteTopUp L342-413）
- Test: 追加到 `new-api/model/topup_audit_test.go`

**Interfaces:**
- Consumes: 无新接口；在既有事务内加 FOR UPDATE 读 user。

- [ ] **Step 1: 写失败测试**

追加到 `topup_audit_test.go`：

```go
func TestManualCompleteTopUp_RecordsBalanceSnapshot(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 52000000)
	tu := newPendingTopUp(t, u.Id, "MANUAL-1")
	require.NoError(t, ManualCompleteTopUp("MANUAL-1", "127.0.0.1"))
	got := GetTopUpByTradeNo("MANUAL-1")
	require.NotNil(t, got)
	assert.Equal(t, int64(52000000), got.BalanceBefore)
	assert.Equal(t, int64(52000000+10*int64(common.QuotaPerUnit)), got.BalanceAfter)
	assert.Equal(t, u.Username, got.CreditedUsername)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./model/ -run TestManualCompleteTopUp_RecordsBalanceSnapshot -v`
Expected: FAIL（BalanceBefore 为 0）

- [ ] **Step 3: 实现**

`ManualCompleteTopUp` 事务内，`tx.Model(&User{}).Where(...).Update("quota", ...)`（L396）前插入：

```go
		user := &User{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", topUp.UserId).First(user).Error; err != nil {
			return err
		}
		topUp.BalanceBefore = user.Quota
		topUp.BalanceAfter = user.Quota + int64(quotaToAdd)
		topUp.CreditedUsername = user.Username
		topUp.CreditedEmail = user.Email
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./model/ -run "TestManualCompleteTopUp|TestCompleteTopUpWithAudit" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add new-api/model/topup.go new-api/model/topup_audit_test.go
git commit -m "feat(model): manual complete records balance snapshot"
```

---

### Task 5: 前端 — 类型 + 订单卡片审计明细区

**Files:**
- Modify: `new-api/web/default/src/features/wallet/types.ts`（TopupRecord L271-290）
- Modify: `new-api/web/default/src/features/wallet/components/dialogs/billing-history-dialog.tsx`（Details Grid 后 L257）

**Interfaces:**
- Consumes: Task 1 的 JSON 字段（channel_trade_no 等）。
- Produces: `TopupRecord` 扩展字段（Task 6 i18n key 引用其标签文案）。

- [ ] **Step 1: 扩展类型**

`TopupRecord` 追加（全部可选，老订单为空）：

```ts
  /** 渠道交易号:微信 transaction_id / 支付宝 trade_no */
  channel_trade_no?: string
  /** 付款人:微信 payer.openid / 支付宝 buyer_id */
  payer_id?: string
  /** 支付宝脱敏账号(可空) */
  payer_account?: string
  /** 入账前余额(quota) */
  balance_before?: number
  /** 入账后余额(quota) */
  balance_after?: number
  /** 入账时刻用户名快照 */
  credited_username?: string
  /** 入账时刻邮箱快照(微信注册用户为空) */
  credited_email?: string
  /** 渠道侧支付时间(unix 秒,0=无) */
  channel_pay_time?: number
  /** 支付提供方:wechat/alipay/alipay_agent/... */
  payment_provider?: string
```

- [ ] **Step 2: 卡片加审计明细区**

`billing-history-dialog.tsx` 在 Details Grid（L228-257）之后、Admin Actions 之前插入。文件顶部 import 补 `ChevronDown, ChevronUp`（lucide-react）。组件内加 `const [expandedId, setExpandedId] = useState<number | null>(null)`。

渠道标签 helper（放组件外）：

```ts
const getChannelTradeNoLabel = (provider?: string) => {
  if (provider === 'wechat') return t('WeChat Pay Transaction ID')
  if (provider === 'alipay' || provider === 'alipay_agent')
    return t('Alipay Transaction ID')
  return t('Channel Transaction ID')
}
```

明细区 JSX（仅成功订单且有任一审计字段时渲染）：

```tsx
{record.status === 'success' &&
  (record.channel_trade_no || record.payer_id || record.balance_before != null) && (
  <div className='mt-3'>
    <button
      type='button'
      onClick={() => setExpandedId(expandedId === record.id ? null : record.id)}
      className='text-muted-foreground flex items-center gap-1 text-xs'
    >
      {expandedId === record.id ? (
        <ChevronUp className='h-3 w-3' />
      ) : (
        <ChevronDown className='h-3 w-3' />
      )}
      {t('Payment Details')}
    </button>
    {expandedId === record.id && (
      <div className='mt-2 space-y-2 rounded-md bg-muted/40 p-3'>
        {record.channel_trade_no && (
          <div className='flex items-center gap-2'>
            <Label className='text-muted-foreground w-32 shrink-0 text-xs'>
              {getChannelTradeNoLabel(record.payment_provider)}
            </Label>
            <code className='truncate font-mono text-xs'>{record.channel_trade_no}</code>
            <Button variant='ghost' size='sm' className='h-5 w-5 p-0'
              onClick={() => copyToClipboard(record.channel_trade_no!)}>
              {copiedText === record.channel_trade_no ? (
                <Check className='h-3 w-3' />
              ) : (
                <Copy className='h-3 w-3' />
              )}
            </Button>
          </div>
        )}
        {record.payer_id && (
          <div className='flex items-center gap-2'>
            <Label className='text-muted-foreground w-32 shrink-0 text-xs'>{t('Payer')}</Label>
            <span className='truncate text-xs'>
              {record.payment_provider === 'wechat' ? `OpenID ${record.payer_id}` : `${t('Buyer ID')} ${record.payer_id}`}
              {record.payer_account ? ` (${record.payer_account})` : ''}
            </span>
          </div>
        )}
        <div className='flex items-center gap-2'>
          <Label className='text-muted-foreground w-32 shrink-0 text-xs'>{t('Credited Account')}</Label>
          <span className='truncate text-xs'>
            {record.credited_username || `#${record.user_id}`}
            {record.credited_email ? ` (${record.credited_email})` : ''} #{record.user_id}
          </span>
        </div>
        {record.balance_before != null && (
          <div className='flex items-center gap-2'>
            <Label className='text-muted-foreground w-32 shrink-0 text-xs'>{t('Balance Before')}</Label>
            <span className='text-xs'>
              {formatCurrencyFromUSD(record.balance_before, { digitsLarge: 2, digitsSmall: 2, abbreviate: false })}
            </span>
          </div>
        )}
        {record.balance_after != null && (
          <div className='flex items-center gap-2'>
            <Label className='text-muted-foreground w-32 shrink-0 text-xs'>{t('Balance After')}</Label>
            <span className='text-xs'>
              {formatCurrencyFromUSD(record.balance_after, { digitsLarge: 2, digitsSmall: 2, abbreviate: false })}
            </span>
          </div>
        )}
        {record.status === 'success' && record.complete_time ? (
          <div className='flex items-center gap-2'>
            <Label className='text-muted-foreground w-32 shrink-0 text-xs'>{t('Credited At')}</Label>
            <span className='text-xs'>{formatTimestamp(record.complete_time)}</span>
          </div>
        ) : null}
        {!!record.channel_pay_time && (
          <div className='flex items-center gap-2'>
            <Label className='text-muted-foreground w-32 shrink-0 text-xs'>{t('Channel Paid At')}</Label>
            <span className='text-xs'>{formatTimestamp(record.channel_pay_time)}</span>
          </div>
        )}
      </div>
    )}
  </div>
)}
```

（无邮箱回退：`credited_email` 为空则不渲染括号，上面 JSX 已覆盖。）

- [ ] **Step 3: 类型检查**

Run: `npm run build`（含 tsc；或 `npx tsc --noEmit`）
Expected: 通过

- [ ] **Step 4: 提交**

```bash
git add new-api/web/default/src/features/wallet/types.ts new-api/web/default/src/features/wallet/components/dialogs/billing-history-dialog.tsx
git commit -m "feat(wallet): audit detail section in billing history"
```

---

### Task 6: i18n 中英文文案

**Files:**
- Modify: `new-api/web/default/src/i18n/locales/en.json`
- Modify: `new-api/web/default/src/i18n/locales/zh.json`

- [ ] **Step 1: 加 key**

两文件顶层对象各追加（按字母序插入）：

```
Alipay Transaction ID: "Alipay Transaction ID" / "支付宝交易号"
Balance After: "Balance After" / "充值后余额"
Balance Before: "Balance Before" / "充值前余额"
Buyer ID: "Buyer ID" / "买家ID"
Channel Paid At: "Channel Paid At" / "渠道支付时间"
Channel Transaction ID: "Channel Transaction ID" / "渠道交易号"
Credited Account: "Credited Account" / "到账账户"
Credited At: "Credited At" / "到账时间"
Payment Details: "Payment Details" / "支付详情"
Payer: "Payer" / "付款人"
WeChat Pay Transaction ID: "WeChat Pay Transaction ID" / "微信支付订单号"
```

Task 7/8 需要的 key 一并加（避免二次改动）：

```
WeChat Register: "WeChat Register" / "微信注册"
Alipay Register: "Alipay Register" / "支付宝注册"
Coming Soon: "Coming Soon" / "即将支持"
Registration Methods: "Registration Methods" / "注册方式"
管理员关闭了微信注册(后端文案,不进 i18n)
```

- [ ] **Step 2: 校验 JSON 合法**

Run: `node -e "JSON.parse(require('fs').readFileSync('src/i18n/locales/en.json'));JSON.parse(require('fs').readFileSync('src/i18n/locales/zh.json'));console.log('ok')"`
Expected: `ok`

- [ ] **Step 3: 提交**

```bash
git add new-api/web/default/src/i18n/locales/en.json new-api/web/default/src/i18n/locales/zh.json
git commit -m "i18n: add audit + register-method keys (en/zh)"
```

---

### Task 7: 微信注册独立开关（后端）

**Files:**
- Modify: `new-api/common/constants.go`（L94 附近）
- Modify: `new-api/model/option.go`（L47 注册区 + L338 update case）
- Modify: `new-api/controller/wechat.go`（L93）
- Modify: `new-api/controller/wechat_oa_identity.go`（L302）
- Modify: `new-api/controller/misc.go`（L94 附近）
- Test: `new-api/model/wechat_option_test.go`（追加）

**Interfaces:**
- Produces: `common.WeChatRegisterEnabled bool`（默认 true）；status 接口字段 `wechat_register_enabled`（Task 8 前端依赖）。

- [ ] **Step 1: 写失败测试**

`wechat_option_test.go` 追加：

```go
func TestWechatRegisterEnabledOptionRegistered(t *testing.T) {
	if _, ok := common.OptionMap["WeChatRegisterEnabled"]; !ok {
		t.Fatal("WeChatRegisterEnabled not registered in OptionMap")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./model/ -run TestWechatRegisterEnabledOptionRegistered -v`
Expected: FAIL

- [ ] **Step 3: 实现**

1. `common/constants.go`：`var WeChatRegisterEnabled = true`（放 `RegisterEnabled` 旁）。
2. `model/option.go`：注册区加 `common.OptionMap["WeChatRegisterEnabled"] = strconv.FormatBool(common.WeChatRegisterEnabled)`；`updateOptionMap` 加：

```go
		case "WeChatRegisterEnabled":
			common.WeChatRegisterEnabled = boolValue
```

3. `controller/wechat.go` L93：`if common.RegisterEnabled {` → `if common.RegisterEnabled && common.WeChatRegisterEnabled {`；else 分支 message 改 `"管理员关闭了微信注册"`。
4. `controller/wechat_oa_identity.go` L302：同条件；message 改 `"管理员关闭了微信注册"`。
5. `controller/misc.go` status map：`"wechat_register_enabled": common.WeChatRegisterEnabled,`（放 `password_register_enabled` 后）。

- [ ] **Step 4: 跑测试确认通过 + 回归**

Run: `go test ./model/ -run "TestWechat" -v && go build ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add new-api/common/constants.go new-api/model/option.go new-api/model/wechat_option_test.go new-api/controller/wechat.go new-api/controller/wechat_oa_identity.go new-api/controller/misc.go
git commit -m "feat(auth): independent WeChatRegisterEnabled toggle"
```

---

### Task 8: 注册开关 — 设置 UI + 前台入口 + 支付宝占位

**Files:**
- Modify: `new-api/web/default/src/features/system-settings/types.ts`（AuthSettings L132-134 附近）
- Modify: `new-api/web/default/src/features/system-settings/auth/section-registry.tsx`（L33-41）
- Modify: `new-api/web/default/src/features/system-settings/auth/index.tsx`（defaults L29-31 附近）
- Modify: `new-api/web/default/src/features/system-settings/auth/basic-auth-section.tsx`（schema L47-49 + 表单 L138-159 附近）
- Modify: `new-api/web/default/src/features/auth/types.ts`（status 类型 L127-130、L170-173 两处）
- Modify: `new-api/web/default/src/features/auth/` 下微信注册/登录入口组件（grep `wechat_login` 定位）

- [ ] **Step 1: 类型与注册表**

1. `system-settings/types.ts` AuthSettings 加 `WeChatRegisterEnabled: boolean`。
2. `section-registry.tsx` BasicAuthSection defaultValues 加 `WeChatRegisterEnabled: settings.WeChatRegisterEnabled,`。
3. `auth/index.tsx` defaults 加 `WeChatRegisterEnabled: true,`。
4. `auth/types.ts` 两处 status 接口加 `wechat_register_enabled?: boolean`。

- [ ] **Step 2: 表单 schema 与 UI**

`basic-auth-section.tsx`：
1. zod schema 加 `WeChatRegisterEnabled: z.boolean(),`。
2. 在 `RegisterEnabled`（L138）与 `PasswordRegisterEnabled`（L159）字段块之后，按既有 Switch 字段模式新增：

```tsx
<FormField
  control={form.control}
  name='WeChatRegisterEnabled'
  render={({ field }) => (
    <FormItem className='flex items-center justify-between rounded-lg border p-3'>
      <div>
        <FormLabel>{t('WeChat Register')}</FormLabel>
        <FormDescription>
          {t('Allow new users to sign up by scanning the WeChat QR code')}
        </FormDescription>
      </div>
      <FormControl>
        <Switch checked={field.value} onCheckedChange={field.onChange} />
      </FormControl>
    </FormItem>
  )}
/>
{/* 支付宝注册预留位:纯 UI,后端未实现 */}
<FormItem className='flex items-center justify-between rounded-lg border p-3 opacity-50'>
  <div>
    <FormLabel>{t('Alipay Register')}</FormLabel>
    <FormDescription>{t('Coming Soon')}</FormDescription>
  </div>
  <FormControl>
    <Switch checked={false} disabled />
  </FormControl>
</FormItem>
```

（`t('Allow new users to sign up by scanning the WeChat QR code')` 及 en/zh key 一并补入 Task 6 的两个 locale 文件。）
3. 分组标题：在 RegisterEnabled 字段块前插入 `<FormLabel className='text-sm font-medium'>{t('Registration Methods')}</FormLabel>`（沿用该文件现有排版元素；若无合适组件用普通 div）。

- [ ] **Step 3: 前台注册入口隐藏**

grep -r `wechat_login` `new-api/web/default/src/features/auth/`：找到微信登录/注册入口渲染处，把可见条件从 `wechat_login` 改为 `wechat_login && status.wechat_register_enabled !== false`（仅影响"注册新号"语义的入口展示；纯登录入口若与注册共用，则保留登录可用性判断不变——若共用且无法区分，隐藏整个入口，因为该场景管理员已显式关闭微信注册）。

- [ ] **Step 4: 类型检查**

Run: `npm run build`
Expected: 通过

- [ ] **Step 5: 提交**

```bash
git add new-api/web/default/src/features/system-settings new-api/web/default/src/features/auth
git commit -m "feat(auth-ui): register methods group with wechat toggle + alipay placeholder"
```

---

### Task 9: 全量验证

**Files:** 无新改动（只验证 + 修复发现的问题）

- [ ] **Step 1: Go 全量**

Run（在 `e:\savvy\new-api`）：`go build ./... && go test ./model/ ./controller/ -count=1`
Expected: 全部 PASS

- [ ] **Step 2: 前端全量**

Run（在 `e:\savvy\new-api\web\default`）：`npm run build`
Expected: 通过

- [ ] **Step 3: 冒烟自查（代码层面核对清单）**

- 微信回调：transaction_id / payer.openid / success_time 落库 ✓；IncreaseUserQuota 不再单独调用 ✓
- 支付宝回调：trade_no / buyer_id / gmt_payment 落库 ✓；存证 goroutine 保留 ✓
- 游客单（user_id=0）：只标记成功、balance 字段为 0 ✓
- 补单：内部有余额快照、前端无新增标识 ✓
- epay/stripe/creem/waffo 路径未被触碰 ✓
- `WeChatRegisterEnabled=false` 时微信注册两处均拒绝、登录不受影响 ✓

- [ ] **Step 4: 提交（如有修复）并汇报**

```bash
git add -A && git commit -m "chore: verification fixes for topup audit records"
```
