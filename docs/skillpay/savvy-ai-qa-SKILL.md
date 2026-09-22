---
name: savvy-ai-qa
slug: savvy-ai-qa
displayName: Savvy 付费 AI 问答
version: 1.0.0
summary: "单次 AI 问答（0.1 元/次）：无需注册与 API Key，Agent 发起请求 → 用户微信授权支付 → 自动返回一次对话补全结果。由 Savvy（new-api）SkillPay 链路支撑。"
license: MIT
---

# Savvy 付费 AI 问答（Pay Skill）

单次 AI 问答，0.1 元/次。基于微信支付 Agent Pay X402 协议。

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

### 第一步：请求资源

```
POST https://scheng.net/api/skill/invoke
Content-Type: application/json

{"query": "用户的问题"}
```

### 第二步：处理 402 响应

收到 HTTP 402 时，提取并保存：
- **Header `WeixinPay-Required`**：支付码（paymentCode）
- **Header `X-Out-Trade-No`**：商户订单号

两者都必须保存，缺一不可。

### 第三步：发起支付

将 `WeixinPay-Required` 的值作为 `paymentCode`，调用 `weixinpay_pay` 向用户申请支付授权。

### 第四步：支付成功后获取资源（⚠️ 必须执行）

```
POST https://scheng.net/api/skill/invoke
WeixinPay-Required: <第二步保存的支付码>
X-Out-Trade-No: <第二步保存的订单号>
Content-Type: application/json

{"query": "原始问题"}   ← body 与第一步完全一致
```

⚠️ **第四步是最容易被 Agent 遗漏的一步**：不携带 `X-Out-Trade-No` Header 重试，就永远拿不到付费内容。

支付成功后返回 HTTP 200：

```json
{
  "code": "SUCCESS",
  "message": "付费内容获取成功",
  "out_trade_no": "WX402_20260922120000abcdef123456",
  "transaction_id": "4200001234202609220000000001",
  "content": "【AI 回答】...",
  "already_fulfilled": false
}
```

支付未完成时返回 402 `PAYMENT_NOT_COMPLETED`（携当前 trade_state），等待用户完成支付后重试。

**payment_code 时效**：支付码最长 15 分钟。超时未支付（或原订单已关闭）时，**回到第一步重新发起请求**生成新订单，不要复用过期的 `WeixinPay-Required` 值。
