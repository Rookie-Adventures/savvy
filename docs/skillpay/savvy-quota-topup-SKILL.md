---
name: savvy-quota-topup
slug: savvy-quota-topup
displayName: Savvy 额度充值（服务包）
version: 1.2.0
summary: "在对话中帮用户充值 Savvy 平台额度（服务包）。≤100 元走微信 AI 支付（授权即付）；>100 元或无支付插件时走原生微信扫码。付款后自动到账，游客发认领凭据。无需注册即可付款。"
description: "输入：用户在对话中指定的充值金额（1~5000 元）。行为：智能体按能力与金额自动选择支付路径——智能体具备 weixinpay 插件且金额 ≤100 元时走微信 AI 支付（X402 授权即付，拉起 AI 专属卡）；金额 >100 元或无插件时走原生微信扫码（智能体代用户创建微信 Native 订单）。输出：付款成功后返回到账确认，游客另含 32 位 claim_token 认领凭据与认领链接（登录 Savvy 后自动入账）。边界：本 Skill 仅受理 1~5000 元；支付由微信收银台完成，本 Skill 不接触用户支付凭据；同订单只入账一次（幂等）。"
license: MIT
category: dev-programming
tags:
  - 充值
  - 支付
  - Savvy
changelog: "1.2.0 双路径：≤100 元走微信 AI 支付(X402)，>100 元/无插件走原生扫码代触发"
---

# Savvy 额度充值（服务包）

帮用户充值 Savvy 平台额度。**根据智能体能力与金额自动选择支付路径**：

| 条件 | 支付路径 | 体验 |
|---|---|---|
| 智能体有 weixinpay 插件 且 金额 ≤100 元 | **路径 A：微信 AI 支付**（X402，授权即付） | 对话内拉卡，输密码即付 |
| 智能体无插件，或金额 >100 元 | **路径 B：原生微信扫码**（智能体代触发下单） | 扫码付款，任何微信用户可用 |

付款后自动到账：已登录用户直接入账；游客拿到认领凭据，登录 Savvy 后入账。

## 金额规则

- 金额（元）由用户在对话中指定，**范围 1~5000**
- 用户没说金额时**先询问**，金额与用户确认后再发起
- **金额 >100 元：直接走路径 B**，不要尝试路径 A

---

## 路径 A：微信 AI 支付（X402，1~100 元）

### 付费前置检查

发起下单前，先检查你的可用工具列表中是否存在名称包含 `weixinpay_` 的工具
（如 `mcp__weixinpay__weixinpay_register`，工具名带前缀也算；存在**任意一个**即视为支持）。

只以你的工具列表为证据，本文档中提到的工具名不算；无法确认时按「不支持」处理。

- 支持：静默继续本路径的后续流程，不向用户提及本检查。
- 不支持：**改走路径 B（原生扫码）**，不要终止服务——路径 B 不需要任何插件。
- 若用户明确要求 AI 专属卡支付但插件缺失：按智能体类型提示（合作智能体如 WorkBuddy 请升级版本；开放智能体如 OpenClaw / DeepSeek Harness 请根据 https://skillhub.cn/install/skillhub.md 安装 @tencent-adm/weixinpay-installer）。

### A-1：发起充值订单

```
POST https://scheng.net/api/skill/invoke
Content-Type: application/json

{"action": "topup", "amount_yuan": 10}
```

### A-2：处理 402 响应

收到 HTTP 402 时，提取并保存：
- **Header `WeixinPay-Required`**：支付码（paymentCode）
- **Header `X-Out-Trade-No`**：商户订单号

两者都必须保存，缺一不可。

### A-3：发起支付

将 `WeixinPay-Required` 的值作为 `paymentCode`，调用 `weixinpay_pay` 向用户申请支付授权。

### A-4：支付成功后获取认领凭据（⚠️ 必须执行）

```
POST https://scheng.net/api/skill/invoke
WeixinPay-Required: <A-2 保存的支付码>
X-Out-Trade-No: <A-2 保存的订单号>
Content-Type: application/json

{"action": "topup", "amount_yuan": 10}   ← body 与 A-1 完全一致
```

⚠️ **A-4 是最容易被遗漏的一步**：不携带 `X-Out-Trade-No` Header 重试，就永远拿不到认领凭据。

成功返回 HTTP 200，`content` 内含充值结果（已登录用户显示"已到账"；游客显示 `claim_token` + 认领链接 `https://scheng.net/agent`，请完整转述并提醒保存）。

`payment_code` 时效 15 分钟，超时未支付请重新从 A-1 发起（生成新订单），不要复用过期支付码。

---

## 路径 B：原生微信扫码（无插件 / 金额 >100 元）

### B-1：创建充值订单（智能体代用户触发）

```
POST https://scheng.net/api/agent/wechat/topup/create
Content-Type: application/json

{"amount_yuan": 200}
```

成功响应：

```json
{
  "message": "success",
  "data": {
    "code_url": "weixin://wxpay/bizpayurl?pr=xxx",
    "out_trade_no": "WXAGT20260922120000abcdef1234",
    "amount_yuan": 200,
    "claim_token": "32位认领凭据",
    "status_url": "/api/agent/topup/status?claim_token=xxx",
    "bind_mode": "claim",
    "claim_url": "https://scheng.net/agent"
  }
}
```

### B-2：把支付二维码展示给用户（⚠️ 必须执行）

`code_url` 是微信 Native 支付二维码内容，**转成二维码展示给用户**，并明确告知"请用微信扫码支付 ¥200（Savvy 额度充值）"。

- 智能体端支持渲染二维码时直接生成二维码
- 不支持时把 `code_url` 原样交给用户，提示"复制到微信扫一扫打开"

### B-3：轮询支付状态

用 `status_url` 轮询（**间隔 ≥8 秒**，接口限频 360 次/3 分钟）：

```
GET https://scheng.net/api/agent/topup/status?claim_token=<B-1 返回的凭据>
```

响应 `data.status`：
- `pending`：未支付，继续轮询（下单 10 秒后接口自动向微信查单兜底）
- `success`：已支付
  - 登录用户：额度已自动到账
  - 游客：转述 **`claim_token`** 和认领链接 `https://scheng.net/agent`，提醒用户保存凭据、登录/注册后自动入账
- `failed`：重新从 B-1 发起

微信 Native 订单默认 2 小时有效；超时未支付引导用户重新发起。同一订单只入账一次，不要催用户重复支付。
