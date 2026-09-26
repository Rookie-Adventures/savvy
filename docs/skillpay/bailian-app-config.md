# 百炼「栗橙科技助手」应用配置包

> **本文件是唯一权威版本**（两个会话的产出已合并；工具数 8 个，含 `listRefunds`）。
> 后续只改这一份，不要另开副本。改前先 `git diff` 看清对方动了什么。
>
> 用途：把百炼上的智能体应用改造成能真正为 Savvy 客户充值/查额度的助手。
> 最后核实：2026-09-26，全部结论基于实测，非推测——
> ① `/api/user/agent/*` 8 条路由均已部署（无一 404），旧前缀 `/api/agent/*` 已 404；
> ② `create` 无 token 返回 `401 缺少或错误的 X-Agent-Token`（鉴权已生效）；
> ③ `status` 不校验 token，参数错误也返回 **HTTP 200**。
>
> 结构：〇 决策 → ① 控制台步骤 → ② 提示词 → ③ 插件表单清单 → 附录 接口事实 → 已知限制。
> 注意：**百炼不认 OpenAPI**，第三节才是可执行内容，附录 YAML 只是留档。

---

## 〇、先做一个决定：支付宝 MCP 留不留

本文件**没有**（也无法）移除支付宝官方 MCP——第二节只是提示词层的软约束。真摘要在控制台做。
而只要它和 `createWechatTopUp` 同时挂在**同一个应用**上，"充值"意图就会在两个工具之间竞争，
模型挑哪个不可控，提示词压不住。这不是配置错误，是工具选择的固有行为。

**建议：拆成两个应用，不要二选一。**

| | 「栗橙科技助手」演示版 | 「Savvy 额度管家」生产版 |
|---|---|---|
| 支付宝 AI 付 MCP | **保留** | **解绑** |
| 本文件 7 个工具 | 不挂 | 全挂 |
| 用途 | 给客户/销售看"说一句话就出支付页" | 真客户实际充值、查额度、退款 |
| 出码归属 | 支付宝官方主体（`app_id=2021005149694034`），**不进 Savvy 账** | 你们自己的 Native 单，可入账可退款 |
| 应用简介必须写 | **"演示用，不代表真实收款通道，请勿付款"** | 正常对外 |

演示版保留支付宝 MCP 是有道理的：你们支付宝产品**未开通**，那条链接大概率走不通付款，
演示"能出支付页"的视觉成本低、效果直观，且不依赖 `AGENT_TOPUP_TOKEN`。
但它的两个代价要认：① 页面文案是支付宝通用样式，不是你们的品牌与商户名；
② 万一哪天真开通且客户真付了，那笔钱在你们系统里**查不到也退不了**——
它的 `out_trade_no` 形如 `recharge_20_20260926`（金额+日期，无用户身份、不唯一），
而你们自己的单号规则是 `WXAGT{14位时间}{10位随机}` / `ALIPAYUSR{userId}NO…`，两套对不上。

> 如果不拆应用，就必须解绑支付宝。**不要**指望靠提示词把两个支付工具隔开。

---

## 一、控制台操作步骤（按顺序）

1. **解绑支付宝官方 MCP**（仅生产版需要；做演示版则保留，并按上表在简介里标注"演示用"）。百炼控制台 → 应用数据 → 「栗橙科技助手」→ 插件 / MCP 服务列表 → 移除支付宝 AI 付相关服务。
   原因：你们支付宝产品**未开通**，该服务是支付宝官方用自己的主体（`app_id=2021005149694034`，非你们商户号）签单，客户付的钱进不了你们的账、也没有 `out_trade_no` 关联可退。解绑无副作用，不涉及签约解除。
2. **创建自定义插件**，按第三节逐项填表单。
   ⚠️ 百炼**不支持导入 OpenAPI / Swagger**（官方《自定义插件》开发指南已确认），只能用自有表单逐个工具手填，或从云市场导入已有 API。第三节那份 YAML 现在只作为我们自己仓库的接口事实清单保留，不要拿去百炼粘贴。
3. **插件级鉴权**：开启「是否鉴权」→ 服务级固定 Token → 传入位置 **Header** → 自定义 Header 名 `X-Agent-Token` → 类型选 **basic**（无前缀）→ Token 值填机B `/opt/savvy/deploy/.env` 里的 `AGENT_TOPUP_TOKEN`，两边**必须完全一致**。
   - 该值只填在控制台，不要写进提示词、不要提交进仓库。
   - 用户的 Savvy API Key 是**每个工具的输入参数**（Header 里的 `Authorization`），不占用这个插件级鉴权位——原因见第三节末尾的"双鉴权冲突"说明。
4. 应用提示词替换为第二节整段，**发布**（不发布不生效）。
5. 验收：`bl app call --app-id <生产版应用 ID> --prompt "我额度不够了，想充 1 块钱"`
   ⚠️ 这里的 ID **不是** `cb7afba7673c41d6b06d42172c87a337`——那是现有应用，按第〇节它归**演示版**
   （保留支付宝 MCP、不挂这 8 个工具）。生产版要在控制台新建，建好后用 `bl app list` 取新 ID 再验。
6. 若按第〇节采用「业务透传」，调用时必须带 `biz_params` 注入用户 Key，例如：
   `bl app call --app-id <ID> --prompt "我还剩多少额度" --biz-params '{"Authorization":"Bearer sk-..."}'`
   不带时预期得到 401，那说明透传没生效，不要误判成"接口坏了"。

> ⚠️ 第 5 步会真实创建一笔待支付订单（下限已放开到 0.01 元，就是为小额测试准备的）。订单不付会自行超时，但它是真记录。

---

## 二、提示词（整段复制到应用指令框）

```text
# 角色

你是「Savvy 额度管家」，由郑州市管城回族区栗橙网络科技工作室（个体工商户，对外品牌「栗橙科技」）运营，
服务邮箱 support@scheng.net。你唯一的职责是帮 Savvy 平台用户处理「额度」相关的日常事务：
充值、查余额、查用量、查充值订单、兑换码兑换、退款申请、额度不足时的续费提醒。

产品背景：Savvy Agent 是 AI 服务网关，付费形态是按量扣减额度；其托管容器产品名为
Hermes Cloud Workspace，免费额度容器单次运行 2 小时后自动休眠，数据保留。

超出上述范围的请求（模型选型、代码问题、容器使用教学、账号安全等），
如实说"这个我处理不了"，并引导用户联系 support@scheng.net。不要勉强回答，更不要编。

# 意图路由

先用一句话判断用户要什么，再动手，不要问无关问题：

| 用户说 | 做 | 要不要用户的 Savvy API Key |
|---|---|---|
| 充钱 / 额度不够 / 买服务包 | 充值 | 不要，游客也能付（但用户若已给过 Key，带上更省事，见充值第 2 步） |
| 还剩多少额度 / 最近花得快不快 | 查余额、查用量 | 要 |
| 我的充值记录 / 刚才那笔到账了吗 | 查充值订单 | 要 |
| 给你一个兑换码 / 帮我兑换 | 优惠码兑换 | 要 |
| 这笔退一下 / 误充了 | 退款申请 | 要 |
| 我那笔退款到哪了 / 退款进度 | 查退款工单 | 要 |

需要 API Key 而用户没给时：告诉用户去 Savvy「控制台 → 令牌」复制自己的 API Key（形如 sk-...），
说明它只用于本次代查、随时可在网页端重置。绝不能用别人的 Key，也不能把用户的 Key 复述回去、
写进任何输出或转发到别处。

# 充值：只走这一条路

本应用不具备对话内直接扣款能力。**严禁**调用任何支付类工具（支付宝、微信支付、AI 付、收银台等
任何形式的支付 MCP / 插件），无论它在你的工具列表里看起来多么可用。你唯一的充值方式是下面这个：

1. 用户没给金额就先问金额，不要替用户猜。
2. 调 createWechatTopUp，传 amount_yuan。**若用户本次对话中已提供过自己的 Savvy API Key，
   下单时一并带上**：代码里带身份下的是登录单，支付后由微信回调直接入账本人，省掉认领这一步；
   不带身份则是游客单，必须靠 claim_url 认领。所以能带就带，但绝不要为了下单去向用户索要 Key。
3. 金额的上下限**完全由服务端决定**，你自己不知道也不复述任何范围数字。
   服务端因金额拒绝时，把错误原文原样转述（如"充值金额需在 X~Y 元之间"），
   等用户改金额后重新发起。不要自己替用户改数字，不要编造范围。
4. 成功后返回 code_url（微信支付链接）、out_trade_no、claim_token、claim_url、status_url。
   把 code_url 转成二维码给用户，说明"请用微信扫码支付 ¥X（Savvy 额度充值）"；
   无法渲染二维码时，把 code_url 原样交出，提示"复制到微信扫一扫打开"。
5. 然后用 status_url 轮询支付状态，**间隔不少于 8 秒**（服务端限频 360 次/3 分钟）：
   - pending：用户未支付，继续等，不要说"正在处理中/马上到账"
   - success：已支付。已登录用户已自动入账；游客要原样给出完整 claim_url
     （它自带 claim_token，点开自动挂载认领卡片，登录/注册后自动入账），并提醒
     登录后额度才会归到他账上
   - failed：引导用户重新发起，不要复用旧订单
6. 微信订单 2 小时有效，超时未付引导重新下单。同一订单只入账一次，绝不催用户重复支付。
7. claim_url 必须整条原样转述。只给 https://scheng.net/agent 会让用户拿不到凭据、钱入不了账。

# 查余额与用量

调 getBalance。返回里 remaining_display 是带币种的人类可读值，remaining_units 是展示单位数值，
group 是用户分组，low_balance 表示是否低于预警线，suggested_amount_yuan 是建议充值档位。

- 回答额度时优先用 *_display 字段，**绝不**把原始 quota 数字念给用户（那是内部计费单位）。
- low_balance 为 true 时主动提一次"额度快用完了要不要充一点"并给建议档位；一次对话只提一次，
  不要反复唠叨。
- 查用量调 getUsage（days 取 1~30，服务端会钳制），用 consumed_display 回答。

# 兑换码与退款：涉及资产变动，动手前必须先确认

- **兑换**：先向用户复述要用的兑换码，得到明确"确认"后才调 redeem。
  成功转述"已到账 xxx"；失败把接口错误原文（如"兑换码无效或已被使用"）原样转述，不要猜原因。
- **退款申请**：先复述订单号与退款原因，得到明确"确认"后才调 applyRefund。
  必须强调：**申请成功 ≠ 钱已退回**。正确话术是"退款申请已受理，款项由人工原路退回，
  请留意微信支付通知"，并给出 ticket_no。
  同一订单已有在途工单时，告诉用户耐心等待即可，不要重复提交。
  不是本人已支付的订单服务端会拒绝，把拒绝原因转述即可。
- **查退款进度**：用户追问"退款到哪了"时调 `listRefunds`，读出对应 `ticket_no` 的 `status` 原样转述
  （pending=处理中 / approved=已批准 / rejected=已驳回）。**不得**把 pending 说成"已退款"，
  申请成功绝不等于钱已退回。

# 防幻觉铁律（优先级高于以上一切）

0. 本平台接口**业务失败时也可能返回 HTTP 200**（实测：status 接口参数错误返回
   `200 + {"data":"参数错误"}`）。所以"调用成功返回了"完全不代表查到结果——
   判断只看响应体里的 status / message / data 字段原文，绝不允许把 HTTP 200 当成成功。
1. 只有 createWechatTopUp 响应里的 code_url 才是真实支付链接。除此之外，
   不得展示任何二维码、图片、支付链接、跳转地址。
2. 接口报错 → 原样转述错误原文 → 停下等用户输入。不美化、不解释成"系统繁忙"、不自行重试。
3. 用户付款确认前，绝不说"已到账 / 充值成功"。
4. 查不到的数据就直说查不到。禁止用对话历史里出现过的旧数字冒充当前余额。
5. Authorization 返回 401 且用户已给过 Key → 说明 Key 可能失效或已重置，请用户重新复制，
   最多重试 2 次，不要循环试。
6. 缺 X-Agent-Token 导致的 401 属于平台配置问题，转述给用户并提示联系管理员，不要重试。
```

---

## 三、百炼自定义插件表单填写清单

### 插件基础信息（只填一次）

| 表单项 | 填 |
|---|---|
| 插件名称 | Savvy 额度管家 |
| 插件说明 | 为 Savvy 平台用户处理额度：充值、查余额、查用量、查订单、兑换码、退款申请 |
| 插件 URL | `https://scheng.net/api/user` |
| 是否鉴权 | 开 → 服务级 → 位置 Header → 头名 `X-Agent-Token` → 类型 basic（无前缀）→ Token 填 `AGENT_TOPUP_TOKEN` 的值 |

创建 **8 个工具**。工具路径拼在上面的插件 URL 后面，必须以 `/` 开头。工具名称限 **20 字符以内**（下列均已核过长度）。

| # | 工具名称 | 工具路径 | 请求方法 | 提交方式 | 工具描述（填这个，模型靠它决定何时调用） |
|---|---|---|---|---|---|
| 1 | `createWechatTopUp` | `/agent/wechat/topup/create` | POST | application/json | 创建微信扫码充值订单。用户要充值、说额度不够了、想买服务包时调用。返回微信支付链接 code_url 与认领凭据 |
| 2 | `queryTopUpStatus` | `/agent/topup/status` | GET | — | 查询某笔充值订单的支付状态。刚下完单需要确认用户付没付时调用，轮询间隔不少于 8 秒 |
| 3 | `getBalance` | `/agent/ability/balance` | GET | — | 查询用户剩余可用额度、所属分组和是否触发低余额预警。用户问"还剩多少额度"时调用 |
| 4 | `getUsage` | `/agent/ability/usage` | GET | — | 查询用户近 N 天的额度消耗量与请求次数。用户问"最近用得快不快"时调用 |
| 5 | `listTopupOrders` | `/agent/ability/orders` | GET | — | 查询用户自己的充值订单列表。用户问充值记录、某笔到账没到账时调用 |
| 6 | `redeemCode` | `/agent/ability/redeem` | POST | application/json | 用优惠码兑换额度。会真实入账，属于资产变动操作 |
| 7 | `applyRefund` | `/agent/ability/refund/apply` | POST | application/json | 为某笔已支付订单提交退款申请工单。属于资产变动操作 |
| 8 | `listRefunds` | `/agent/ability/refund/list` | GET | — | 查询用户自己的退款工单列表与处理进度（pending/approved/rejected）。用户问"我那笔退款到哪了 / 退款进度"时调用 |

### 输入参数（逐工具）

**1 createWechatTopUp**

| 参数名称 | 类型 | 传入方法 | 传参方式 | 必填 | 参数描述 |
|---|---|---|---|---|---|
| amount_yuan | Number | Body | 大模型识别 | 是 | 充值金额（单位元）。上下限由服务端裁定，不得自行猜测或代用户修改 |

**2 queryTopUpStatus**

| 参数名称 | 类型 | 传入方法 | 传参方式 | 必填 | 参数描述 |
|---|---|---|---|---|---|
| claim_token | String | Query | 大模型识别 | 是 | 下单接口返回的认领凭据，同时也是本接口的访问凭据 |

**3 getBalance** ／ **5 listTopupOrders** ／ **6 redeemCode** ／ **7 applyRefund** ／ **8 listRefunds** 共用的鉴权参数：

| 参数名称 | 类型 | 传入方法 | 传参方式 | 必填 | 参数描述 |
|---|---|---|---|---|---|
| Authorization | String | Header | 业务透传 | 是 | `Bearer ` + 用户本人的 Savvy API Key |

各工具**额外**业务参数：

| 工具 | 参数名称 | 类型 | 传入方法 | 传参方式 | 必填 | 描述 |
|---|---|---|---|---|---|---|
| getUsage | Authorization | String | Header | 业务透传 | 是 | 同上 |
| getUsage | days | Number | Query | 大模型识别 | 否 | 统计天数，取 1~30，默认 7，服务端会钳制 |
| listTopupOrders | page | Number | Query | 大模型识别 | 否 | 页码，默认 1 |
| listTopupOrders | page_size | Number | Query | 大模型识别 | 否 | 每页条数，默认 10 |
| redeemCode | code | String | Body | 大模型识别 | 是 | 用户提供的优惠码原文 |
| applyRefund | out_trade_no | String | Body | 大模型识别 | 是 | 要退款的订单号，来自订单列表 |
| applyRefund | reason | String | Body | 大模型识别 | 是 | 退款原因，用户的原话 |
| listRefunds | page | Number | Query | 大模型识别 | 否 | 页码，默认 1 |
| listRefunds | page_size | Number | Query | 大模型识别 | 否 | 每页条数，默认 10，最大 50 |

> **为什么 Authorization 选「业务透传」而不是「大模型识别」**：选后者意味着用户的 API Key 要经模型之手生成参数值——Key 会进模型上下文和调用日志，模型也可能复述出来。选业务透传则由调用方在 `biz_params` 里注入，模型全程不知道 Key 内容。**代价**：每次调用应用都必须带上 `biz_params`，微信侧智能体运行时若不支持透传，就只能退回"大模型识别"并接受 Key 进上下文这一风险。这一条要么按透传实现，要么改成让用户在网页端自助操作，不要默默降级。

### 输出参数

百炼要求输出参数**逐项声明且均为必填**，`Object` 类型的**子属性不能为空**，必须逐层展开。按真实响应结构填（实测响应把业务字段嵌在 `data` 里），**不要展平**，否则模型读不到值：

| 工具 | 输出参数结构 |
|---|---|
| createWechatTopUp | `message`(String)、`data`(Object) → 子字段 `code_url` / `out_trade_no` / `claim_token` / `claim_url` / `status_url` / `bind_mode` 均 String |
| queryTopUpStatus | `message`(String)、`data`(Object) → 子字段 `status`(String)、金额相关字段按实际返回补 |
| getBalance | `message`(String)、`data`(Object) → 子字段 `remaining_display` / `remaining_units` / `group` / `low_balance` / `recharge_url` / `suggested_amount_yuan` |
| getUsage | `message`(String)、`data`(Object) → 子字段 `consumed_display` / `consumed_quota` / `request_count` |
| listTopupOrders | `message`(String)、`data`(Object) → 子字段 `trade_no` / `money` / `status` / `payment_method` / `create_time` / `complete_time` |
| redeemCode、applyRefund | `message`(String)、`data`(Object) → 兑换填 `quota_display`；退款填 `ticket_no` / `amount_yuan` |
| listRefunds | `message`(String)、`data`(Object) → 子字段 `items`(Array[Object]) → 每个对象含 `ticket_no` / `trade_no` / `amount_yuan` / `reason` / `status` / `create_time` / `source`；外加 `total`(Number)、`page`(Number) |

> ⚠️ 实测：这套接口**业务失败也返回 HTTP 200**，body 形如 `{"data":"参数错误","message":"error"}`。此时 `data` 是字符串而不是对象，与上面声明的 Object 结构冲突。如果百炼因结构不符而报错或让模型误判成功，就把 `data` 声明成 String，改由提示词要求模型读 `message` 判断成败——这条等你真机填的时候验证一次再定稿。

### 高级配置（强烈建议填，降低漏召回）

| 工具 | 用户输入 Query 示例 | 期望构造的入参 Value |
|---|---|---|
| createWechatTopUp | 我额度快用完了，帮我充 50 块 | `{"amount_yuan": 50}` |
| queryTopUpStatus | 我刚那笔付好了吗 | `{"claim_token": "<上次返回里的 claim_token>"}` |
| getBalance | 我还剩多少额度 | `{}` |
| getUsage | 最近七天我用了多少 | `{"days": 7}` |
| listTopupOrders | 看下我的充值记录 | `{"page": 1, "page_size": 10}` |
| redeemCode | 我有个兑换码 SAVVY-ABC123，帮我兑换 | `{"code": "SAVVY-ABC123"}` |
| applyRefund | 早上那笔 20 块充错了，帮我退 | `{"out_trade_no": "WXAGT...", "reason": "误充了"}` |
| listRefunds | 我上次申请的退款到哪了 | `{"page": 1, "page_size": 10}` |

---

## 附录：接口事实清单（YAML，仅本仓库留档，百炼不认）

```yaml
openapi: 3.0.1
info:
  title: Savvy 额度管家能力层
  version: 2.0.1
servers:
  - url: https://scheng.net
components:
  securitySchemes:
    agentToken:
      type: apiKey
      in: header
      name: X-Agent-Token
    userKey:
      type: http
      scheme: bearer
      description: 用户本人的 Savvy API Key（控制台→令牌 复制，形如 sk-...）
paths:
  /api/user/agent/wechat/topup/create:
    post:
      operationId: createWechatTopUp
      summary: 创建微信扫码充值订单，返回支付链接与认领凭据
      # 带 userKey 时为登录单（支付后直接入账），不带时为游客单（需 claim_url 认领）
      security:
        - { agentToken: [] }
        - { agentToken: [], userKey: [] }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [amount_yuan]
              properties:
                amount_yuan:
                  type: number
                  description: 用户自定义充值金额（元）。上下限由服务端裁定
      responses:
        "200": { description: 下单成功，data 内含 code_url/out_trade_no/claim_token/claim_url/status_url }
  /api/user/agent/topup/status:
    get:
      operationId: queryTopUpStatus
      summary: 查询充值订单支付状态（轮询间隔不少于 8 秒）。无需 X-Agent-Token，
        claim_token 本身即能力凭据；HTTP 200 不代表成功，须读 body 里的 status
      parameters:
        - in: query
          name: claim_token
          required: true
          schema: { type: string }
      responses:
        "200": { description: pending / success / failed }
  /api/user/agent/ability/balance:
    get:
      operationId: getBalance
      summary: 查询剩余额度、分组与低余额预警
      security: [{ userKey: [] }]
      responses:
        "200": { description: remaining_quota/remaining_units/remaining_display/group/low_balance/recharge_url/suggested_amount_yuan }
  /api/user/agent/ability/usage:
    get:
      operationId: getUsage
      summary: 查询近 N 天额度消耗
      security: [{ userKey: [] }]
      parameters:
        - in: query
          name: days
          required: false
          schema: { type: integer, default: 7, minimum: 1, maximum: 30 }
      responses:
        "200": { description: consumed_quota/consumed_display/request_count }
  /api/user/agent/ability/orders:
    get:
      operationId: listTopupOrders
      summary: 查询本人充值订单记录
      security: [{ userKey: [] }]
      parameters:
        - { in: query, name: page, schema: { type: integer, default: 1 } }
        - { in: query, name: page_size, schema: { type: integer, default: 10 } }
      responses:
        "200": { description: trade_no/money/status/payment_method/create_time/complete_time }
  /api/user/agent/ability/redeem:
    post:
      operationId: redeemCode
      summary: 兑换优惠码（会真实入账，执行前必须向用户复述兑换码并得到确认）
      security: [{ userKey: [] }]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [code]
              properties:
                code: { type: string }
      responses:
        "200": { description: quota / quota_display }
  /api/user/agent/ability/refund/apply:
    post:
      operationId: applyRefund
      summary: 提交退款申请工单（申请成功不等于已退款）
      security: [{ userKey: [] }]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [out_trade_no, reason]
              properties:
                out_trade_no: { type: string }
                reason: { type: string }
      responses:
        "200": { description: ticket_no / amount_yuan }
  /api/user/agent/ability/refund/list:
    get:
      operationId: listRefunds
      summary: 查询本人退款工单列表与处理进度（pending/approved/rejected）
      security: [{ userKey: [] }]
      parameters:
        - { in: query, name: page, schema: { type: integer, default: 1 } }
        - { in: query, name: page_size, schema: { type: integer, default: 10, maximum: 50 } }
      responses:
        "200": { description: items[]/total/page；items 含 ticket_no/trade_no/amount_yuan/reason/status/create_time/source }
```

## 已知限制

- **🔴 P0：客户端 IP 可任意伪造 → 限流失效 + 审计失真**（与智能体无关，现在就在影响线上）。
  new-api 全仓无一处 `SetTrustedProxies`，gin 因此按默认策略信任所有代理并取 `X-Forwarded-For`
  **左段**当客户端 IP；而机A nginx 用的是 `$proxy_add_x_forwarded_for`（**追加**，不覆盖），
  也没有 `real_ip_header` / `set_real_ip_from`。于是任何调用方自带
  `X-Forwarded-For: 1.2.3.4` 就会被原样留在链首、被 `c.ClientIP()` 采信：
  · 换一个头值＝换一个全新限流桶，Critical / Global 档对注册、登录、兑换、退款、下单形同不存在；
  · `middleware/audit.go:136` 记进审计日志的 IP 就是调用方自报的，事后追溯不可信——
    对要处理误充、退错单争议的业务，这条比限流本身严重。
  **修法必须两侧同时动**：机A nginx 把 XFF 改为 `$remote_addr` 覆盖链首；
  new-api 显式 `SetTrustedProxies(["172.24.96.232/32"])`（机A VPC 地址）。
  ⚠️ 只改 nginx 那半边会让所有请求的 ClientIP 恒等于机A，直接演变成下面 P1 的共桶问题。
- **🟠 P1：按 IP 共桶，接入百炼后会成为容量瓶颈**。`middleware/rate-limit.go:24` 的键是
  `"rateLimit:" + mark + c.ClientIP()`——**只按 IP，不含路径**，所以：
  1. 所有挂 `CriticalRateLimit()` 的路由（`create` / `redeem` / `applyRefund` / `claim` / `register`）
     **共用同一个桶**，不是各自一份；`refund/list` 未挂 Critical，不受影响。
  2. P0 修好之后真实 IP 才会浮现——那时百炼插件出口那一小撮共享 IP 就会让**经由智能体的全体客户
     挤在同一桶里**（Critical 档 20 次 / 20 分钟，见 router 注释），现象是"时好时坏下不了单"。
  处理方向（择一，需你定）：按 `X-Agent-Token` 维度而非 IP 限流（推荐，网页端与智能体互不影响）、
  把这几个路由降到 Global 档、或在机A 侧对百炼出口 IP 放行。**不要**直接调大全局阈值。
  顺序上：P0 不修，P1 的容量测算没有意义——因为现在的"没被限流"是假象。
- **百炼不支持导入 OpenAPI**（官方《自定义插件》开发指南确认），第三节表单是唯一的落地方式。
  附录 YAML 只作本仓库的接口事实清单。
- **双鉴权塞不进一个插件级鉴权位**：百炼的插件鉴权只能配一套，所以 `X-Agent-Token` 占掉插件级，
  用户 `Authorization` 只能做成逐工具的 Header 输入参数。选「业务透传」可让 Key 不进模型上下文，
  但要求调用方每次带 `biz_params`；若宿主不支持透传，需明确接受"Key 进上下文"的风险或改由网页端自助。
- **输出参数结构可能与真实响应不匹配**：业务失败时 `data` 是字符串（实测 `{"data":"参数错误"}`），
  而成功时是 Object。百炼要求 Object 必须逐层展开子字段且出参均必填。真机填一次验证，
  若解析失败就把 `data` 降级声明为 String，靠提示词读 `message` 判成败。
- 本配置只覆盖**路径 B（微信扫码）**。路径 A（X402 对话内授权即付，≤100 元）要求宿主平台自带
  `weixinpay_*` 工具，百炼不提供，因此在这份提示词里被明确禁用。要上路径 A，智能体必须跑在
  微信侧的 agent 平台上。
- 路径 A 另有一个未收口的设计问题：它要求智能体把一次性支付凭据 `WeixinPay-Required` 逐字符回传，
  而微信支付官方规范明确禁止该凭据经模型之手（正常路径由宿主"工程化支付"代码自动接管，
  凭据改动任何一个字符即 `401 PAYMENT_CODE_INVALID`，后果是用户付了钱拿不到 `claim_token`）。
  建议改法：履约依据换成服务端主动向微信查单（路径 B 已有"下单 10 秒后自动查单兜底"这套现成逻辑），
  模型只带 `out_trade_no`。修好后路径 A 才适合开放给客户。
- `claim_token` 是**能力凭据**：状态接口不校验 `X-Agent-Token`，谁拿到 token 就能读到该订单的
  支付状态与金额。因此 `claim_url` 只能交给下单者本人，不要出现在群发话术、日志或截图里。
- `refund/list`（查自己的工单）**已纳入第 8 个工具 `listRefunds`**（`GET /api/user/agent/ability/refund/list`）。
  注意后端该路由（api-router.go:139）**未挂** `CriticalRateLimit()`，因此**不受**上文 IP 共桶限流影响——
  查退款不会被智能体下单流量拖垮，也不会因为共享 IP 而 429。
- 若百炼的自定义插件不允许 `X-Agent-Token` 这个自定义头名，先告诉我，不要改成把 token 塞进
  URL 或提示词——那会让密钥出现在日志和对话记录里。
