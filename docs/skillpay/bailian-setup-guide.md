# 百炼「创建插件」配置指南 — Savvy 额度管家

> 适用：在百炼(Bailian)后台用「创建插件」手动把 Savvy 后端接口做成 HTTP 工具。
> 配套文件：`bailian-openapi.yaml`（可导入版）、`savvy-quota-topup-bailian.zip`（技能包 v2.0.1）。
> 生产域名：`https://scheng.net`

## 0. 先分清「配什么」

- 百炼「创建插件」页配置的是**你自己的 HTTP 接口**（= 声明 HTTP 工具），**可以自己配**。
- **微信支付官方插件（weixinpay / MCP）不能在这里创建**，百炼也装不了它。
- 百炼上你的「微信支付」= 调用你的后端 `topup/create` → 拿到 `code_url`（微信二维码）→ 用户扫码付款。**无需任何微信插件**。

## 1. 一个插件只能有一套 Header / 鉴权 → 建两个插件

| 插件 | 覆盖接口 | 鉴权 |
|---|---|---|
| **A. Savvy 充值** | 下单、查订单状态 | Header `X-Agent-Token: 3c0877b2ae2b5399708597d578daa051`（平台级固定值，可写死） |
| **B. Savvy 账户** | 余额 / 用量 / 订单 / 兑换 / 退款 | 用户 API Key：`Authorization: Bearer sk-...`（运行时由终端用户提供，**禁止写死**） |

## 2. 插件 A「Savvy 充值」逐字段

- **插件名称**：`Savvy 充值`
- **插件描述**：用户要充值 Savvy 额度时调用；创建微信扫码订单并返回二维码；可查询订单状态。
- **插件URL**：`https://scheng.net`
  （若表单要求单个接口的完整地址，则填 `https://scheng.net/api/user/agent/wechat/topup/create`）
- **Header列表**：Key = `X-Agent-Token`，Value = `3c0877b2ae2b5399708597d578daa051`
- **是否鉴权**：关（token 已写在 Header 里）
- 点「继续添加工具」追加：
  - `GET /api/user/agent/topup/status`，参数 `claim_token`（必填，string）

### 两个工具规格
1. `createWechatTopUp` — `POST /api/user/agent/wechat/topup/create`
   - body: `{"amount_yuan": <number>}`（金额由用户指定，服务端裁定上下限；百炼走路径B，任意金额含 >100 均可）
   - 返回: `code_url`（微信二维码，**只有它是真二维码**）/ `claim_token` / `claim_url` / `out_trade_no`
2. `queryTopUpStatus` — `GET /api/user/agent/topup/status?claim_token=<token>`
   - 返回: `status` = `pending` / `success` / `failed`

## 3. 插件 B「Savvy 账户」逐字段

- **插件名称**：`Savvy 账户`
- **插件描述**：查余额 / 查近N天用量 / 查充值订单 / 兑换优惠码 / 申请退款 / 查退款工单。
- **插件URL**：`https://scheng.net`
- **是否鉴权**：开，方式 = API Key（Header `Authorization`）
  - Value = 用户的 `Bearer sk-...`；用「增加输入参数」把它做成**运行时参数**，由用户对话时提供，**勿写死**。
- 点「继续添加工具」逐个追加以下 6 个：

| operationId | 方法 + 路径 | 参数 |
|---|---|---|
| `getBalance` | `GET /api/user/agent/ability/balance` | — |
| `getUsage` | `GET /api/user/agent/ability/usage` | `days`(1~30, query) |
| `getOrders` | `GET /api/user/agent/ability/orders` | `page`, `page_size`(query) |
| `redeemCode` | `POST /api/user/agent/ability/redeem` | body `{"code": "<string>"}` |
| `applyRefund` | `POST /api/user/agent/ability/refund/apply` | body `{"out_trade_no": "<str>", "reason": "<str>"}` |
| `listRefunds` | `GET /api/user/agent/ability/refund/list` | `page`, `page_size`(query) |

## 4. 更省事：导入 OpenAPI

若百炼插件页支持「导入 API 文档 / OpenAPI」，直接上传 `bailian-openapi.yaml`，可一次性生成上述 8 个工具并带两套鉴权 scheme，省去手动逐个添加。

## 5. 注意

- 能力层的用户 API Key 是**每个终端用户自己的**，必须运行时提供；**不要**把它写死成平台 token。
- 下单接口的 `X-Agent-Token` 是**平台级固定值**，可以写死。
- 金额上下限由服务端裁定，工具描述里不要写死数字。
- 技能侧铁律：响应里**只有 `code_url` 才是真二维码**，其余字段一律不展示。

## 6. ⚠️ 建完插件 ≠ 能用：必须在智能体应用里「挂载」插件

**实测坑（2026-09-26 百炼调试记录）**：技能上传后，agent 已能正确判断
「当前环境没有配置微信支付插件，将使用原生扫码通道」，但紧接着报：

> 抱歉，当前智能体未配置充值工具（缺少 `createWeChatTopUp` 等 HTTP 工具），无法直接创建充值订单。

**原因**：在「插件管理」里建好插件只是一半，**必须回到智能体应用的编排页把该插件添加/开启**，agent 才能调用。
注意报错里点名的 `createWeChatTopUp` 正是 `bailian-openapi.yaml` 里的 operationId——说明技能侧工具名是对的，纯粹是百炼侧工具没挂上。

**正确顺序**：
1. 插件管理 → 创建插件 A「Savvy 充值」→ 加工具 `createWechatTopUp` / `queryTopUpStatus` → 保存并**发布插件**
2. 插件管理 → 创建插件 B「Savvy 账户」→ 加 6 个工具 → 保存并**发布插件**
3. 打开**智能体应用 → 编排/编辑** → 在「插件 / 工具」区域 → **添加刚建的两个插件**（确认已授权/启用）
4. **发布应用** → 再问「生成个微信1元测试订单」，应能返回 `code_url` 二维码

**自检口诀**：agent 说「缺少 `createWeChatTopUp`」= 第 3 步没做（或插件只保存未发布）。
