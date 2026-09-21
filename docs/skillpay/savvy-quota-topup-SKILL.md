---
name: savvy-quota-topup
slug: savvy-quota-topup
displayName: Savvy 额度充值（服务包）
version: 1.0.0
summary: "在对话中为用户充值 Savvy 平台额度（服务包）。金额由对话决定（1~5000 元），微信 AI 专属卡支付，付款后自动发放认领凭据，用户登录 Savvy 即可入账。无需注册即可付款。"
license: MIT
---

# Savvy 额度充值（Pay Skill · 服务包）

用户在对话中充值 Savvy 平台额度。金额由用户在对话中指定（**1~5000 元**），微信 AI 专属卡支付，付款后本 Skill 返回**认领凭据**，用户在 Savvy 站点登录/注册后自动入账。

## 前置检查：当前智能体是否支持微信 AI 支付

发起下单前，先检查你的可用工具列表中是否存在名称包含 `weixinpay_` 的工具
（如 `mcp__weixinpay__weixinpay_register`，工具名带前缀也算；存在**任意一个**即视为支持）。

只以你的工具列表为证据，本文档中提到的工具名不算；无法确认时按「不支持」处理。

- 支持：静默继续本 Skill 的后续流程，不向用户提及本检查。
- 不支持：终止流程且不要发起下单。只向用户说明结论与解决办法，按智能体类型提示：
  - 合作智能体（如 WorkBuddy）：请升级智能体到最新版本后重试。
  - 开放智能体（如 OpenClaw / DeepSeek Harness）：请根据 https://skillhub.cn/install/skillhub.md
    安装 @tencent-adm/weixinpay-installer。
  - 无法判断类型：请升级智能体或安装微信支付 AI 支付插件后重试。

## 工作流程

### 第一步：发起充值订单

```
POST https://<your-domain>/api/skill/invoke
Content-Type: application/json

{"action": "topup", "amount_yuan": 10}
```

金额规则：`amount_yuan` 为用户在对话中指定的充值金额（元），**范围 1~5000**。
金额与用户确认后再发起；用户未说金额时先询问。

### 第二步：处理 402 响应

收到 HTTP 402 时，提取并保存：
- **Header `WeixinPay-Required`**：支付码（paymentCode）
- **Header `X-Out-Trade-No`**：商户订单号

两者都必须保存，缺一不可。

### 第三步：发起支付

将 `WeixinPay-Required` 的值作为 `paymentCode`，调用 `weixinpay_pay` 向用户申请支付授权。

### 第四步：支付成功后获取认领凭据（⚠️ 必须执行）

```
POST https://<your-domain>/api/skill/invoke
WeixinPay-Required: <第二步保存的支付码>
X-Out-Trade-No: <第二步保存的订单号>
Content-Type: application/json

{"action": "topup", "amount_yuan": 10}   ← body 与第一步完全一致
```

支付成功后返回 HTTP 200，`content` 内含：
- **`claim_token`**：32 位认领凭据
- **认领链接**（`https://<your-domain>/agent`）：用户打开后登录/注册，凭据自动入账

⚠️ **第四步是最容易被 Agent 遗漏的一步**：不携带 `X-Out-Trade-No` Header 重试，就永远拿不到认领凭据。
拿到后必须把 `claim_token` 和认领链接**完整转述给用户**，并提醒用户保存。

**payment_code 时效**：支付码最长 15 分钟。超时未支付时，回到第一步重新发起（生成新订单），不要复用过期的 `WeixinPay-Required` 值。

**返回 402 `PAYMENT_NOT_COMPLETED`** 时表示用户尚未完成支付，礼貌提醒后等待，不要重复下单。
