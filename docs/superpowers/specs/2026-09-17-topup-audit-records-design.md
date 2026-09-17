# 充值订单审计记录补齐（支付宝/微信审核取证）— 设计文档

日期：2026-09-17
分支：`feat/topup-audit-records`（自 `dev` 切出）

## 1. 背景与目标

支付宝/微信支付平台审核（商户申诉、交易核验）时，需要证明"这笔支付对应的额度真的发给了该用户"。当前 `topups` 表只有业务单号（`trade_no`）、金额、时间、状态，审核人员无法：

- 核对支付平台的订单号（微信 `transaction_id` / 支付宝 `trade_no`）与商户号流水对账；
- 看到付款人是谁；
- 确认额度发给了哪个账户、入账前后余额变化；
- 拿到完整时间线。

**目标**：让订单历史中的支付宝/微信充值记录成为审计级证据链，审核人员一眼看懂。

**核心受众**：支付宝/微信审核人员。管理员补单属内部操作，不做展示标识。

## 2. 非目标（Non-Goals）

- 不改海外渠道（stripe / creem / waffo / epay）的入账逻辑，其记录新字段为空，前端展示"—"；
- 不展示蚂蚁链存证信息（保留现有 `SubmitOrderEvidenceFn` 上报代码不动，仅不展示）；
- 管理员补单不加任何 UI 标识（内部记录仍补齐前后余额，保证数据完整性）；
- 不做记录导出/下载功能。

## 3. 数据模型变更（`topups` 表新增列）

GORM `AutoMigrate` 自动加列，无需手工迁移。老订单新字段为零值，前端展示"—"。

| 字段 | 类型 | JSON | 含义 |
|---|---|---|---|
| `ChannelTradeNo` | varchar(64), index | `channel_trade_no` | 渠道交易号：微信 `transaction_id` / 支付宝 `trade_no` |
| `PayerId` | varchar(128) | `payer_id` | 付款人：微信 `payer.openid` / 支付宝 `buyer_id` |
| `PayerAccount` | varchar(128) | `payer_account` | 支付宝 `buyer_logon_id`（脱敏账号，回调有才填，可空） |
| `BalanceBefore` | bigint | `balance_before` | 入账前用户 quota 快照 |
| `BalanceAfter` | bigint | `balance_after` | 入账后用户 quota 快照（= before + 本次额度） |
| `CreditedUsername` | varchar(255) | `credited_username` | 入账时刻用户名快照 |
| `CreditedEmail` | varchar(255) | `credited_email` | 入账时刻邮箱快照 |
| `ChannelPayTime` | bigint | `channel_pay_time` | 渠道侧支付时间：微信 `success_time` / 支付宝 `gmt_payment`（秒级时间戳，0 表示无） |

注意：遵循 `new-api/AGENTS.md`，不使用 GORM boolean default 类 tag；新列不加 `default:true` 一类标签。

## 4. 后端变更

### 4.1 新增 model 层函数：`CompleteTopUpWithAudit`

`new-api/model/topup.go` 新增：

```go
type TopUpAudit struct {
    ChannelTradeNo string
    PayerId        string
    PayerAccount   string
    ChannelPayTime int64
}

func CompleteTopUpWithAudit(tradeNo string, expectedProvider string, audit TopUpAudit) error
```

单事务（`DB.Transaction`）内完成，替换现有"先 `topUp.Update()` 再异步 `IncreaseUserQuota`"的两步式入账：

1. `FOR UPDATE` 读 topup，校验存在性、`PaymentProvider` 匹配、幂等（已 success 直接返回 nil）；
2. `FOR UPDATE` 读 user 拿当前 quota（`balance_before`）；
3. 计算 `quotaToAdd`（逻辑与现有一致：非 Stripe 用 `Amount * QuotaPerUnit`）；
4. 更新 topup：`status=success`、`complete_time=now`、`channel_trade_no`、`payer_id`、`payer_account`、`channel_pay_time`、`balance_before`、`balance_after=balance_before+quotaToAdd`、`credited_username/email` 快照；
5. 同事务内 `Update("quota", gorm.Expr("quota + ?", quotaToAdd))` 更新用户余额。

**顺带修复已知隐患**：现有微信/支付宝/epay 回调的 `topUp.Update()` 成功但 `IncreaseUserQuota` 失败时"钱到账未加额度"的 parity 问题（`topup_wechat.go` L198、`topup_alipay.go` L215 有 ponytail 标注），事务化后不再存在。

### 4.2 微信回调（`controller/topup_wechat.go` WechatNotify）

`finalize` 中解析 `payload`（明文 JSON，已在手）：

```go
type wxTopUpNotifyDetail struct {
    TransactionId string `json:"transaction_id"`
    SuccessTime   string `json:"success_time"` // RFC3339，解析失败置 0
    Payer         struct {
        Openid string `json:"openid"`
    } `json:"payer"`
}
```

构造 `TopUpAudit` 后调用 `CompleteTopUpWithAudit(tradeNo, model.PaymentProviderWechat, audit)`；成功后 `RecordTopupLog` 保持原文案不变。删除单独的 `IncreaseUserQuota` 调用。

### 4.3 支付宝回调（`controller/topup_alipay.go`）

- 普通单：从回调表单取 `trade_no`、`buyer_id`、`buyer_logon_id`、`gmt_payment`（`yyyy-MM-dd HH:mm:ss` 转秒级时间戳），调 `CompleteTopUpWithAudit`；`SubmitOrderEvidenceFn` 上报逻辑原样保留（fire-and-forget，在事务外）。
- 智能体单（`completeAgentTopUp`）：同样回填 `ChannelTradeNo/PayerId/PayerAccount/ChannelPayTime`；游客单（`user_id=0`）`balance_*` 置 0。入账部分同样事务化（复用 `CompleteTopUpWithAudit` + agent 特有的金额回填逻辑，如需可加参数或在函数内按 provider 分支）。

### 4.4 管理员补单（`model.ManualCompleteTopUp`）

在现有事务内增加：读 user quota（FOR UPDATE）→ 记录 `balance_before/balance_after/credited_username/credited_email`。无渠道信息则不填。UI 不变。

### 4.5 查询接口

`GetUserTopUps / GetAllTopUps / Search*` 返回整个 `TopUp` 结构体，新字段自动带出，**controller/router 无需改动**。

## 5. 前端变更（`web/default/src/features/wallet/`）

### 5.1 类型（`types` / API 层）

`TopUpRecord` 增加上述 8 个字段（可空，老订单为 `null`/0）。

### 5.2 订单卡片（`billing-history-dialog.tsx`）

每张订单卡片在现有"支付方式 / 金额 / 支付金额"网格下方新增**审计明细区**（成功订单展示；渠道字段为空则整行显示"—"）：

- **渠道交易号**：按 `payment_provider` 显示标签——`wechat` → "微信支付订单号"，`alipay`/`alipay_agent` → "支付宝交易号"，其他 → "渠道交易号"；带复制按钮（复用 `useCopyToClipboard`）；
- **付款人**：微信显示 `OpenID: xxx`；支付宝显示 `买家ID: xxx`（有 `payer_account` 时追加展示）；
- **到账账户**：`username (email) #id`，email 为空则 `username #id`；
- **充值前余额 / 充值后余额**：用 `formatCurrencyFromUSD` 格式化（与 Amount 一致）；
- **下单时间 / 到账时间 / 渠道支付时间**：`formatTimestamp`，渠道时间为 0 则不展示该行。

所有用户可见（用户看自己的记录，管理员看全部），非仅管理员——审核场景可能是用户截图自己的记录提交。

### 5.3 i18n

新增文案同步 en/ja/fr/ru/vi/zh 六个 locale（`web/default/src/i18n/locales/`）：渠道交易号、微信支付订单号、支付宝交易号、付款人、买家ID、到账账户、充值前余额、充值后余额、渠道支付时间、到账时间等。

## 6. 展示效果（最终形态）

微信单：

```
WXUSR1024NOAb3dEf1737123456  [复制]           ● 充值成功
2026-09-17 14:30:05
支付方式: 微信支付    充值额度: $100.00    支付金额: ¥103.00
─────────────────────────────────────────────
微信支付订单号: 4200002376202609173XXXXXXXXX  [复制]
付款人: OpenID oX-8k5Rr9xQ...
到账账户: licheng (user@example.com) #1024
充值前余额: $52.00
充值后余额: $152.00
到账时间: 2026-09-17 14:30:42    渠道支付时间: 2026-09-17 14:30:40
```

支付宝单：`支付宝交易号 2026091722001...`、`买家ID 2088xxxx`、（如有）脱敏账号 `138****1234`。

## 7. 错误处理

- `CompleteTopUpWithAudit` 任何一步失败 → 整体回滚，回调返回 fail，渠道会重试；
- 微信 `success_time` 解析失败 → `ChannelPayTime=0`，不阻断入账；
- 支付宝 `gmt_payment` 同理；
- 老订单/未覆盖渠道字段为空 → 前端"—"，不影响展示。

## 8. 测试

- **Go 单测**（sqlite + AutoMigrate `TopUp`/`User`，沿用 `controller/payment_safety_gates_test.go` 模式）：
  - `CompleteTopUpWithAudit`：成功路径（status/审计字段/用户 quota 均正确）、幂等（二次调用不重复加钱）、provider 不匹配报错、pending 校验；
  - 微信 notify payload 解析：`transaction_id`/`payer.openid`/`success_time` 提取；
  - 支付宝表单提取：`trade_no`/`buyer_id`/`buyer_logon_id`/`gmt_payment` 转时间戳。
- **前端**：`tsc` + build 通过；手动核对中英文展示与"—"降级。

## 9. 实施顺序

1. model 层：结构体字段 + `CompleteTopUpWithAudit`（含单测）
2. 微信回调接入（含 payload 解析单测）
3. 支付宝回调接入（普通单 + agent 单，含表单提取单测）
4. `ManualCompleteTopUp` 补余额快照
5. 前端类型 + 卡片审计明细区
6. i18n 六语言
7. 全量构建验证（go test / go build / web build）
