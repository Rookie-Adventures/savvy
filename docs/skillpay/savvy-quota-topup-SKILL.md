---
name: savvy-quota-topup
slug: savvy-quota-topup
displayName: Savvy 额度管家（充值·查询·兑换·退款）
version: 2.0.0
summary: "照顾 Savvy 平台额度的一切对话需求：充值（≤100 元走微信 AI 支付，否则走原生扫码）、查余额与用量、查充值订单、优惠码兑换、退款申请、续费引导。写入类操作需用户 API Key，支付由微信收银台完成，本 Skill 不接触支付凭据。"
description: "输入：用户的意图与必要参数（充值场景为 1~5000 元金额，其他场景为用户 API Key 或订单号）。行为：先按意图路由——充值/查余额/查用量/查订单/兑换码/退款申请；充值按金额与插件能力选择微信 AI 支付（≤100 元、授权即付）或原生微信扫码（代触发 Native 订单），其余意图调用对应能力层接口并用用户身份（Authorization: Bearer <access_token>）鉴权。输出：人话结果；充值成功返回到账确认（游客另含 claim_token 与认领直达链接），退款返回工单号。边界：金额仅受理 1~5000 元；严禁虚构二维码/支付链接/到账状态；涉及资产变动的一步操作（兑换、退款申请）必须先征得用户确认；同一订单只入账一次（幂等）。"
license: MIT
category: dev-programming
tags:
  - 充值
  - 支付
  - Savvy
  - 额度管家
changelog: "2.0.0 多意图能力层：查余额/用量/订单、优惠码兑换、退款申请(工单)、低余额续费提醒、认领后引导，统一走用户 API Key 鉴权；1.2.4 认领链接带 claim_token；1.2.3 工具优先(createWechatTopUp/queryTopUpStatus)；1.2.2 修正接口路径前缀(/api/user/agent/...)；1.2.1 防幻觉铁律 + X-Agent-Token 鉴权说明"
---

# Savvy 额度管家

一个 Skill 管完 Savvy 额度的日常：**充值**、**查余额/用量/订单**、**兑换码**、**退款申请**、**续费提醒**。

## 意图路由（先判断用户要什么，再动手）

| 用户说 | 走哪一节 | 是否需要 API Key |
|---|---|---|
| 充钱 / 额度不够了 / 买服务包 | [路径 A/B：充值](#充值) | 否（游客也能付） |
| 还剩多少额度 / 我用了多少 / 最近花得快不快 | [查余额与用量](#查余额与用量) | 是 |
| 我的充值记录 / 刚才那笔到账了吗 | [查充值订单](#查充值订单) | 是 |
| 给个兑换码 / 兑换 CDK | [优惠码兑换](#优惠码兑换) | 是 |
| 这笔钱退一下 / 误充了 | [退款申请](#退款申请) | 是 |

**拿不到 API Key 时**：告诉用户去 Savvy「控制台 → 令牌」复制自己的 API Key（形如 `sk-...`），并说明它只用于本次代查、随时可在网页端重置。**不要用别人的 Key，也不要让 Key 出现在给第三方的话术里。**

## 通用约定

### 用户身份（写入/查询类能力）

涉及**个人账户数据**或**资产变动**的能力必须带用户身份：

```
Authorization: Bearer <用户的 Savvy API Key>
```

- 未带 → 接口返回 401：如实告诉用户"需要提供你的 Savvy API Key 才能继续"
- 返回 401 且用户已给过 Key → 说明 Key 可能失效/重置了，请用户重新复制，不要重试超过 2 次

### 平台鉴权（如平台已配置环境变量）

若智能体平台为本 Skill 配置了环境变量 `AGENT_TOPUP_TOKEN`，则**下单类请求**必须携带：

```
X-Agent-Token: <AGENT_TOPUP_TOKEN 的值>
```

缺少或值错误时返回 401，把错误转述给用户并提示联系管理员，不要自行重试。（能力层的查询/兑换/退款只认用户 API Key，不强求这个头。）

### 防幻觉铁律（优先级最高）

1. **只有接口响应里的 `code_url` 才是真实支付二维码**，除此之外不得展示任何二维码/图片/支付链接
2. 接口报错 → 原样转述错误 → 停止等待用户输入
3. 用户付款确认前，不得宣称"已到账/充值成功"
4. **退款申请成功 ≠ 钱已退回**：只能说"已受理，款项由人工原路退回"，给出 `ticket_no`
5. 查不到的数据就直说查不到，不得用历史对话里的旧数字冒充当前余额

---

# 充值

金额（元）由用户指定，**范围 1~5000**；用户没说金额先询问，确认后再发起。金额 >100 元**只能走路径 B**。

| 条件 | 支付路径 | 体验 |
|---|---|---|
| 智能体有 weixinpay 工具 且 金额 ≤100 元 | **路径 A：微信 AI 支付**（X402，授权即付） | 对话内拉卡，输密码即付 |
| 无插件，或金额 >100 元 | **路径 B：原生微信扫码** | 扫码付款，任何微信用户可用 |

## 路径 A：微信 AI 支付（X402，1~100 元）

### A-0：付费前置检查

检查可用工具列表中是否存在名称包含 `weixinpay_` 的工具（如 `mcp__weixinpay__weixinpay_register`，带前缀也算，存在任意一个即视为支持）。只以工具列表为证据，本文档提到的工具名不算；无法确认时按「不支持」处理。

- 支持：静默继续，不向用户提及本检查
- 不支持：**改走路径 B**，不要终止服务

### A-1：发起充值订单

```
POST https://scheng.net/api/skill/invoke
Content-Type: application/json

{"action": "topup", "amount_yuan": 10}
```

### A-2：处理 402 响应

保存响应头：`WeixinPay-Required`（支付码）与 `X-Out-Trade-No`（订单号），缺一不可。

### A-3：发起支付

以 `WeixinPay-Required` 为 `paymentCode` 调用 `weixinpay_pay` 向用户申请支付授权。

### A-4：支付成功后获取认领凭据（⚠️ 必须执行）

```
POST https://scheng.net/api/skill/invoke
WeixinPay-Required: <A-2 保存的支付码>
X-Out-Trade-No: <A-2 保存的订单号>
Content-Type: application/json

{"action": "topup", "amount_yuan": 10}   ← body 与 A-1 完全一致
```

⚠️ 不携带 `X-Out-Trade-No` 重试，永远拿不到认领凭据。

成功返回 HTTP 200，`content` 内含结果：已登录用户显示"已到账"；游客显示 `claim_token` + 认领直达链接 `https://scheng.net/agent?claim_token=<32位>&out_trade_no=<订单号>`——**原样转述整条链接**，点开即自动挂载认领卡片，登录/注册后自动入账，用户不必手抄凭据。

`payment_code` 时效 15 分钟，超时请重新从 A-1 发起（生成新订单），不要复用过期支付码。

## 路径 B：原生微信扫码（无插件 / 金额 >100 元）

### B-1：创建充值订单

优先调用工具 `createWechatTopUp`（或名称含 `topup` 的 HTTP 工具）传 `amount_yuan`；工具不可用时自行请求：

```
POST https://scheng.net/api/user/agent/wechat/topup/create
Content-Type: application/json

{"amount_yuan": 200}
```

⚠️ 若当前环境没有任何可发起 HTTP 请求的工具，也不要让用户去网页自己操作——先说明"当前智能体未配置充值工具，请联系管理员添加 createWechatTopUp 工具"。

成功响应：

```json
{
  "message": "success",
  "data": {
    "code_url": "weixin://wxpay/bizpayurl?pr=xxx",
    "out_trade_no": "WXAGT20260922120000abcdef1234",
    "amount_yuan": 200,
    "claim_token": "32位认领凭据",
    "status_url": "/api/user/agent/topup/status?claim_token=xxx",
    "bind_mode": "claim",
    "claim_url": "https://scheng.net/agent?claim_token=xxx&out_trade_no=WXAGT..."
  }
}
```

⚠️ `claim_url` 已自带 `claim_token`，**原样转述给用户**（只给 `https://scheng.net/agent` 会让用户拿不到凭据、钱入不了账）。

### B-2：展示支付二维码（⚠️ 必须执行）

把 `code_url` 转成二维码给用户，并说明"请用微信扫码支付 ¥200（Savvy 额度充值）"。无法渲染二维码时把 `code_url` 原样交出，提示"复制到微信扫一扫打开"。

### B-3：轮询支付状态

优先调用 `queryTopUpStatus`（传 `claim_token`）；不可用则用 `status_url` 轮询（**间隔 ≥8 秒**，限频 360 次/3 分钟）：

```
GET https://scheng.net/api/user/agent/topup/status?claim_token=<凭据>
```

- `pending`：未支付，继续轮询（下单 10 秒后服务端自动向微信查单兜底）
- `success`：已支付。登录用户已自动到账；游客转述 **`claim_url`** 并附 **`claim_token`** 原文，提醒登录/注册后入账
- `failed`：重新从 B-1 发起

微信 Native 订单 2 小时有效；超时未支付引导重新发起。同一订单只入账一次，不要催用户重复支付。

---

# 查余额与用量

```
GET https://scheng.net/api/user/agent/ability/balance
Authorization: Bearer <API Key>
```

返回要点：`remaining_quota`(原始额度)、`remaining_units`(展示单位数值)、`remaining_display`(带币种的人类可读值)、`group`、`low_balance`（是否低于预警线）、`recharge_url`、`suggested_amount_yuan`。

**运营动作**：`low_balance = true` 时主动提醒"额度快用完了要不要充一点"，并给出 `suggested_amount_yuan` 的档位；不要反复唠叨，一次对话提一次就够。

```
GET https://scheng.net/api/user/agent/ability/usage?days=7
Authorization: Bearer <API Key>
```

返回近 N 天（**取值 1~30，服务端会钳制**）的 `consumed_quota`、`consumed_display`、`request_count`。回答用量时优先用 `*_display` 字段，别把原始 quota 数字念给用户。

# 查充值订单

```
GET https://scheng.net/api/user/agent/ability/orders?page=1&page_size=10
Authorization: Bearer <API Key>
```

返回 `trade_no / money / status / payment_method / create_time / complete_time`。用户问"刚才那笔到没到账"时，用这里的 `status`（`pending`/`success`/`failed`）回答，**不要**把 `pending` 说成"已经处理中、马上到"。

# 优惠码兑换

⚠️ 兑换会真实入账，**执行前必须向用户复述兑换码并得到确认**。

```
POST https://scheng.net/api/user/agent/ability/redeem
Authorization: Bearer <API Key>
Content-Type: application/json

{"code": "用户的兑换码"}
```

- 成功：返回 `quota`、`quota_display`，转述"已到账 xxx"
- 失败：接口返回的错误原文（"兑换码无效或已被使用"）原样转述，不要猜原因

# 退款申请

⚠️ 退款申请会生成真实工单，**执行前必须复述订单号与原因并得到确认**。

```
POST https://scheng.net/api/user/agent/ability/refund/apply
Authorization: Bearer <API Key>
Content-Type: application/json

{"out_trade_no": "WXAGT...", "reason": "误充了"}
```

约束：
- 订单必须是**本人已支付**的单；不是本人的单服务端会拒绝（防止跨账号退错的钱）
- 同一订单已有在途工单时返回"该订单已有在途的退款申请"，告诉用户耐心等待即可，不要重复提交
- 成功后返回 `ticket_no`（工单号）与 `amount_yuan`；话术必须是"退款申请已受理，款项由人工原路退回，请留意微信支付通知"

查询自己的工单：

```
GET https://scheng.net/api/user/agent/ability/refund/list?page=1&page_size=10
Authorization: Bearer <API Key>
```
