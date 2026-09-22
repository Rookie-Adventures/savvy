---
name: savvy-quota-topup
slug: savvy-quota-topup
displayName: Savvy 额度充值（服务包）
version: 1.1.0
summary: "在对话中帮用户充值 Savvy 平台额度（服务包）。金额 1~5000 元由用户指定，智能体代用户创建微信支付订单，用户微信扫码付款，付款后自动到账（游客发认领凭据）。无需注册、无需任何支付插件。"
license: MIT
category: dev-programming
tags:
  - 充值
  - 支付
  - Savvy
changelog: "1.1.0 改为智能体代触发原生微信支付（扫码），任何智能体可用，无需 weixinpay 插件"
---

# Savvy 额度充值（服务包）

帮用户充值 Savvy 平台额度。智能体**代用户创建微信支付订单**，用户**微信扫码付款**（普通微信支付，不是 AI 专属卡），付款后自动到账——已登录用户直接入账，游客拿到认领凭据登录后入账。

**本 Skill 不需要任何支付插件**，所有支持 HTTP 请求的智能体均可使用。

## 金额规则

- `amount_yuan`：用户在对话中指定的充值金额（元），**范围 1~5000**
- 用户没说金额时**先询问**，金额与用户确认后再发起
- 用户付款金额以此为准，到账额度按平台汇率自动换算

## 工作流程

### 第一步：创建充值订单

```
POST https://scheng.net/api/agent/wechat/topup/create
Content-Type: application/json

{"amount_yuan": 10}
```

成功响应：

```json
{
  "message": "success",
  "data": {
    "code_url": "weixin://wxpay/bizpayurl?pr=xxx",
    "out_trade_no": "WXAGT20260922120000abcdef1234",
    "amount_yuan": 10,
    "claim_token": "32位认领凭据",
    "status_url": "/api/agent/topup/status?claim_token=xxx",
    "bind_mode": "claim",
    "claim_url": "https://scheng.net/agent"
  }
}
```

### 第二步：把支付二维码展示给用户（⚠️ 必须执行）

`code_url` 是微信 Native 支付二维码内容，**转成二维码展示给用户**，并明确告知：

> 请用微信扫码支付 ¥10.00（Savvy 额度充值）

- 智能体端支持渲染二维码时，直接用 `code_url` 生成二维码
- 不支持时，把 `code_url` 原样交给用户，提示"复制到微信扫一扫打开"

### 第三步：轮询支付状态

用 `status_url` 轮询（**间隔 ≥8 秒**，接口限频 360 次/3 分钟）：

```
GET https://scheng.net/api/agent/topup/status?claim_token=<第一步返回的凭据>
```

响应 `data.status`：
- `pending`：未支付，继续轮询（下单 10 秒后接口会自动向微信查单兜底）
- `success`：已支付
  - 登录用户：额度已自动到账，告知"充值成功，已到账"
  - 游客：转述 **`claim_token`** 和认领链接 `https://scheng.net/agent`，指导用户"打开链接登录/注册后自动入账"，并提醒保存凭据
- `failed`：下单失败，重新从第一步发起

### 订单有效期与异常处理

- 微信 Native 订单默认 2 小时有效；超时未支付请引导用户重新发起
- 同一笔订单只入账一次（平台幂等保障），不要催用户重复支付
- 轮询 2 小时仍 pending 的单视为放弃，告知用户重新发起
