# 支付宝「AI 收」产品选型取证与第一款实现完整度审计

> 结论速览：JSAPI/百炼 前提不成立；六款产品中当前可开通 3 款；用户选定第一款（AI 网页应用收款），其收款主链路已在生产验证，实现审计缺 3 个 API（退款、退款查询、关闭交易）+ 1 个下单参数（`timeout_express`）——经核 SDK v3.2.29 **全部现成，属未调用而非未实现**。第一款同时是支付宝 Skill Pay（PC 端）的官方前置条件，故选定它即已站在 agent 收款链路的正确起点上。
>
> 本轮已落地：`timeout_express=30m`（网站支付两条分支）+ 锁定测试。**但只闭合了支付宝侧的自动关单，后台堆积的 pending 单不会因此变干净**——真正的收口是「查单 → 标 expired」的清扫，见尾巴第 2 项。

## 症状

2026-10-11 用户提出「发布一个 agent 应用接支付宝的 AI 收」，并给出前提：

> 开通必须要产品 JSAPI，开通 JSAPI 必须智能体要在百炼。

随后贴出「证据」链接：
`https://aipay.alipay.com/docs/vibe-pay/llms.txt?product=AI_WEB_APP_PAY&enterprisePipeline=true`

若前提成立，则 savvy 必须先把智能体托管到阿里云百炼，再去申请 JSAPI 产品——这是一条完全错误的路线。

## 根因：三条都是误读

### 1. 「JSAPI」在官方文档里只出现一次，是费率表的一行，不是开通前提

全量抓取 aipay 官方文档后逐份 grep：

| 文档 | 大小 | `jsapi` 命中 | `百炼` 命中 |
|---|---|---|---|
| 主索引 `aipay.alipay.com/llms.txt` | 6248B | 0 | 0 |
| AI 网页应用收款接入指南 `ai-web-app-payment-integration-guide.md` | 36044B | 0 | 0 |
| 用户贴的 `/docs/vibe-pay/llms.txt` | 3746B | 0 | 0 |
| 智能收款套件产品介绍 `nowpay-intro.md` | 5322B | **2（87、92 行）** | 0 |

唯一出处在即刻收产品介绍的「费率」章节：

```
## 费率
智能收款套件的费率由收单产品（JSAPI支付）和 即刻收商品经营 组成。
| 产品 | 费率 |
| JSAPI支付 | 0.6%~1% |
| 即刻收商品经营 | 0.4% |
```

即 JSAPI 是**即刻收套件底层使用的收单产品，是费率构成项**。商户不需要、也没有入口去单独「开通 JSAPI」。

### 2. 「必须在百炼」来自另一个同名产品

百炼控制台里确实有个开关叫**「支付宝AI收」**，但它是 **AI 打赏**（2025-06-25，支付宝 + 蚂蚁百宝箱 + 阿里云百炼联合发布）：

- 要求智能体托管在百炼
- 钱进开发者**个人**支付宝 AI 钱包，不是商户号
- 金额由平台随机，上限 10 元
- 本质是打赏，不是收单

aipay.alipay.com 的「AI 收」指 **A2M / AI 按量付费**，与百炼零关系。两个「AI 收」同名不同物——与 `docs/records/2026-09-28-x402-openid-appid-space.md` 里微信 X402 的角色错配是同一类命名陷阱。

### 3. 用户贴的 URL，查询参数是摆设，且内容不是 webpay 的

实测 `diff`：

- `?product=AI_WEB_APP_PAY&enterprisePipeline=true`
- 不带任何参数
- `?product=AI_AGENT_PAY`

三者返回**字节完全相同**，md5 均为 `bf40f6f2b775dba222e781fc581d1ff5`，3746 字节。服务端忽略查询参数。

而该文件的真实内容是**智能收款套件-即刻收**的索引（章节标题 `## 智能收款套件-即刻收` / `### 平台模式接入` / `### 直连模式接入`），不是 AI 网页应用收款的索引。

附带发现：官方主索引里给的网页应用文档链接
`/docs/vibe-pay/ai-web-app-payment-qianyi/llms.txt` **是坏的**——返回 SPA 的 HTML 跳转页（`Content-Type: text/html`，`Content-Length: 12198`，Server Tengine），不是 llms.txt。真正的 webpay 指南在 `ai-web-app-payment-integration-guide.md`。

## 生产实况（机B，2026-10-11 只读巡检）

巡检脚本 `deploy/ops/ops_check_alipay.sh`（私钥/公钥一律 `<masked>`，只报 `length(value)`）。

支付宝直连**已配置且已在跑**：

| 项 | 值 |
|---|---|
| `AlipayAppId` | `2021006170668597` |
| `AlipayIsProduction` | `true`（生产网关） |
| 密钥模式 | 公钥模式（应用私钥 len 1624，支付宝公钥 len 392） |
| `AlipayNotifyURL` | `https://scheng.net` |
| `compliance_confirmed` | `true`（约 2026-07-10，user 1，ip 119.13.57.141，terms v1） |

`isAlipayTopUpEnabled()` 的两个条件（合规确认 + `IsAlipayConfigured()`）均满足。

订单：

| provider | 成功 | 金额 | 备注 |
|---|---|---|---|
| `alipay` | 21 | 7438 元 | |
| （空，历史单） | 3 | 417 元 | provider 字段引入前的老单 |
| `alipay_agent` | **0** | 0 元 | 14 笔全 pending，印证 AGENTS.md「百炼托管应用链路已整体退役」 |
| `wechat` | 9 | 716 元 | |
| `wechat_skillpay` | 2 | 2 元 | |

支付宝合计 **24 笔成功 / 7855 元**，55 笔 pending。

代码侧：new-api 全仓只调三个 product_code——`FAST_INSTANT_TRADE_PAY`（PC 网页）、`QUICK_WAP_WAY`（手机网页）、`FACE_TO_FACE_PAYMENT`（扫码）。**没有任何 JSAPI 实现**（`alipay.trade.create` 零引用）。

**结论：AI 网页应用收款的收款能力已在生产生效**。「AI 网页应用收款」是「网站支付」的改名而非新产品——`alipay-aipay` 技能 `references/integration/flow.md:4` 原文：「本 flow 仅支持 AI 网页应用收款、AI 移动应用收款、AI 按量付费三种产品的集成。**原有『网站支付』『APP 支付』『按量付费』仍作为输入别名和资源目录名**」；技能自带代码示例目录字面名为 `2-网站支付`，注释「`FAST_INSTANT_TRADE_PAY` 网站支付固定值，必填」。它用的就是 `alipay.trade.page.pay` + `FAST_INSTANT_TRADE_PAY`，与 `controller/subscription_payment_alipay.go:88` 现有调用完全一致。

收款能力生效的证据是硬的：webpay 指南第 234 行「`product_code` 必填，销售产品码，**与支付宝签约的产品码名称**一致」，而生产上该产品码真实成功 24 笔——未签约则支付宝网关直接拒绝，不由我方推断。

### 但有一个未闭合的缺口

改名不等于不换签约主体。webpay 在技能里对应一个明确的 salesCode：

| 产品 | salesCode | scope |
|---|---|---|
| AI 网页应用收款 | `I1080300001000041203` | `app:all,fast_instant_trade_pay:write` |
| AI 移动应用收款 | `I1080300001000041313` | `app:all,auth_alipay_apppay:write` |
| AI 按量付费 | `I1080300001000160457` | `app:all,machine_pay:write,agmnt:write` |

`onboarding/flow.md:259`：「`SIGNED_EFFECTIVE` 与 `SIGN_SUBMITTED` 均**禁止重复提交签约**」；`:47`：「**已签约时不再要求这组图片**」；`:12`：「每次开始先校验登录、scope 和 MCC，再**查签约状态**……**不得按历史对话推定外部操作结果**」。即该流程本身是「先只读查签约，仅 `NOT_SIGNED` 才走签约」的设计。

**本记录能证明的是「`FAST_INSTANT_TRADE_PAY` 在 app `2021006170668597` 上有效」，未能证明「签约列表中该产品的 salesCode 就是 `I1080300001000041203`」。** 若支付宝将「AI 网页应用收款」发成了新 salesCode（存量网站支付商户需重签一次方进入新体系），则 24 笔成功只证明旧签约有效。该缺口无法从外部证伪，闭合方式二选一：

1. 登录 open.alipay.com，查看 app `2021006170668597` 的已签约产品列表；
2. 在 Mac/Linux 上运行 `alipay-aipay` 技能的 onboarding，discovery 阶段会直接返回 `SIGNED_EFFECTIVE` / `NOT_SIGNED`。

第 2 条正是该技能的设计用途，但在 Windows 上被 `onboarding-bootstrap` 阻断。

## 六类产品：当前可开通性判定

| 产品 | 开通入口 | 材料门槛 | savvy 状态 | 判定 |
|---|---|---|---|---|
| AI 网页应用收款（webpay） | `/open-flow/products` | 3 张网站截图（首页/商品页/支付页） | scheng.net 在跑，支付页现成，24 笔成功 | ✅ 能，且**可能已签**（见上节缺口） |
| AI 按量付费（Machine Pay / A2M） | `/open-flow/products?product=metered` | **无需截图**，全场最低 | `alipay-aipay-build/server.js` 已写完 288 行，**整个目录未入版本控制** | ✅ 能 |
| Token Pay / AI 订阅 | 企业账号 `b.alipay.com/page/product-workspace/solution-detail?solutionCode=SP000001000000101144`；个人账号仅 `/merchant-intent` 联系销售 | **仅企业支付宝账号可一站式接入** | 用户有邮箱登录的企业主体账号（营业执照认证） | ⚠️ 能，但有账号前提 |
| AI 移动应用收款（apppay） | `/open-flow/products` | APP 名称 + 3 张 APP 界面截图，签约状态固定 `OFFLINE` | savvy 是 web SaaS，**无原生 APP**（AGENTS.md 明确不做小程序） | ❌ 缺材料 |
| 即刻收（智能收款套件） | 直连模式=留资表单；平台模式=挂靠合作平台 | **白名单** | 未申请 | ❌ 卡审批 |
| Agent Pay｜智能体支付 | — | — | — | ❌ 方向相反，是**付款方**产品（智能体替用户付钱），非收款 |

Skill Pay｜AI 技能收款（`skillpay.alipay.com`）另计：不在 `alipay-aipay` 技能支持范围（`integration/flow.md:4` 只认网页/移动/按量付费三种），需按产品站自行接入。注意与本项目已有的**微信** SkillPay（`service/skillpay_x402.go`、provider `wechat_skillpay`）不是同一物。

### 两个边界

**1. 技能能代办签约的只有 2 款。** `integration/flow.md:4` 划定的硬范围是 AI 网页应用收款、AI 移动应用收款、AI 按量付费；Token Pay 不在内，走独立的 `b.alipay.com` solution 页，须商户自行在商家平台操作。而这 3 款中移动应用收款缺材料，故**技能实际能跑完签约的 = 网页应用收款 + 按量付费**——偏偏该技能在 Windows 上被 `onboarding-bootstrap` 阻断。

**2. Token Pay 有一个未核前提。** `nowpay-direct.md:47`：「应用调用身份应与商品所属商户一致」；一站式接入只认企业支付宝账号。**app `2021006170668597` 归属个人（个体工商户）还是企业主体账号**，决定 Token Pay 能否复用现有密钥对，还是必须在企业账号下新建应用重配。仅能由用户登录开放平台核实。

### 即刻收与 savvy 额度体系最贴，但有硬伤

`nowpay-direct.md` 第 5 行：

> 直连模式面向独立开发者、创作者或自行经营应用的商户。商户在支付宝管理小程序完成商品配置，应用服务端直接调用即刻收 OpenAPI，为应用用户提供托管购买、权益校验以及次数／积分核销能力。

即**不需要通过合作平台**（`nowpay-intro.md:60` 那句「仅支持创作者通过合作平台接入」说的是平台模式）。但第 7 行：

> 直连模式当前面向**白名单创作者**开放

### 能力映射

| 即刻收 | savvy 现有 |
|---|---|
| `billing_mode=QUANTITY` + `quota_unit=COUNT/POINT` | `users.quota` |
| `quota.query` → `total/used/remaining` | 查余额接口 |
| `purchase.create` → `purchase_url` / `purchase_qr_code` | 充值下单 |
| `quota.verify`（`out_request_no` + `amount` + `consume_reason`） | 扣费 |
| `quota.refresh`（原请求号，幂等） | 掉单补偿 |
| `external_buyer_id` | savvy user id |
| `out_product_id`（`np_d_xxx`，即刻收生成，不可自编） | 套餐 / plan |

托管购买页免开发，权益账本由支付宝托管，退款对账不用自己扛。

### 硬伤：核销是整数，savvy 计费是连续金额

`quota.verify` 的 `amount` 是**整数次数或整数积分**（文档明写「次数核销也必须显式传入 `amount=1`」），而 new-api 的计费是按 token 折算的连续金额（一次对话可能扣 0.0037 元）。要接就得把 savvy 额度重新包装成「积分包」，定义 `1 积分 = 固定 token 量`，改计费口径。这是设计决策，不是接线工作。

另一处：`consume_reason` 上限 256 字符，且**明确要求不得填完整提示词、用户隐私、业务 JSON**，也不保证查询时回传——不能拿它当业务上下文存储。

### 其它边界

- 接入范围**不包含自动续费**（`nowpay-direct.md:30`）→ 订阅仍须 Token Pay
- **不提供商品初始化/配置/启停 OpenAPI**（`:88`）→ 商品只能在手机支付宝「即刻收」小程序里手工配
- 结算：默认实时到账签约账号支付宝余额；但**首次签约收单产品未满 90 日、或连续交易未满 30 日的商家，次日结算**（`nowpay-intro.md:82`）
- 网关 `https://openapi.alipay.com/gateway.do`、版本 `1.0`、`RSA2`/`UTF-8`/JSON——与 new-api 现有 alipay SDK 配置一致，`app_id` 与密钥对可复用
- 前置余额充足**仍需正式核销**，不能只按 `quota.query` 结果做本地扣减
- `remaining` 已排除预占，不能用 `total-used` 替代
- `PROCESSING` 时 `consumed`/`remaining` 可能缺失，**不能补零后推断结果**

## 底座 / 技能 / Agent 三层关系（Skill Pay 定位）

用户提问「我们目前做的是这个技能对吧，并不是 agent」——**两者都不是**。官方 `docs/agent-pay/skillpay.md` 的 PC 端方案第 23-24 行原文：

> 如果你的服务未接入支付宝**电脑网站支付**，请先参考文档接入支付宝电脑网站支付。
> 在接入支付宝电脑网站支付后，这份指南会带你在原有系统外**增加一个「商家下单 Skill」**。完成后，Agent 可以帮用户选择商品、创建订单，并在支付环节把流程交给支付宝支付处理 Skill，最终由支付宝引导用户完成支付。

三层是**叠加**关系，不是互斥选项：

| 层 | 是什么 | savvy 状态 |
|---|---|---|
| 1. 底座 | 电脑网站支付（= AI 网页应用收款），`alipay.trade.page.pay` | ✅ 已有，24 笔生产成功 |
| 2. 技能 | 「商家下单 Skill」，由 Agent 唤起，内部仍调底座 `page.pay`（GET 方式） | ❌ 没有 |
| 3. Agent | 唤起技能的 Agent（支付宝侧 `alipay-bot` / 智能体平台） | ❌ 非我方提供 |

**故第一款不是 agent 方案的替代品，而是其官方前置条件。**

技能层信令（`skillpay.md:30-46`）：取 `page.pay` 返回的 `pageRedirectionData` → `alipay-bot trigger-payment-signal --payment-link <cashier_url> --merchant-info <商户信息,商品名,金额> --amount <金额>` 压成支付短链（因原链接过长）→ `alipay-bot submit-payment --payment-link <短链> --intent-summary <...>` 唤起支付宝 Skill，之后支付宝接管支付流程。联调需 `npx -y @alipay/agent-payment@latest install`。

**移动端方案完全不同**：前置要求「已签约 **APP支付** 收单产品码」（这正解释了 AI 移动应用收款为何要 APP 名称 + 3 张截图）；商户调**预下单** `alipay.trade.order.prepay` 取 `prepayId`，交智能体平台推进支付。savvy 无原生 APP，此路不通。

### 与微信 SkillPay 的结构对照

支付宝「商家下单 Skill」与本项目已有的**微信** SkillPay（X402，`service/skillpay_x402.go`，生产 2 笔成功）是同一思路：技能被 Agent 唤起 → 技能内部下单 → 返回支付入口给 Agent → 平台推进支付 → 回调履约。差别仅在信令载体：

| | 微信 SkillPay | 支付宝 Skill Pay |
|---|---|---|
| 支付入口载体 | HTTP 402 + `WeixinPay-Required` 头 | `pageRedirectionData` / `*url.URL` |
| 推进方式 | Agent 带支付码重试 | `alipay-bot submit-payment` |
| 前置签约 | SkillHub 发布 | 电脑网站支付（已有） |

注意二者同名「Skill Pay」但分属两个平台，不可混用。参见 AGENTS.md 铁律三。

## 开通这步 Agent 做不了

两条阻塞：

1. `alipay-aipay` 技能的产品开通流程**在 Windows 上不支持**——`onboarding-bootstrap` 直接渲染跳转消息并终止 run（`platform_id=aec0b036-dbc8-4510-a79f-271e372bcd44`）。需在 aipay.alipay.com 网站上做，或在 Mac/Linux 上跑该技能。
2. 即刻收直连模式是**白名单 + 手机小程序签约**，无 API 通道。

### 交给用户的操作单

**A. 现在就能办的三款**

> 三个入口 2026-10-11 均实测可达：`/open-flow/products` → 200（171789B，SPA 应用壳）、`/merchant-intent` → 200、`b.alipay.com` solution 页取自 `subscription.md:12,152` 原文。首轮 curl 返回 000 是下节记录的 TLS 抖动，非路径失效。

| 产品 | 入口 | 要准备什么 |
|---|---|---|
| AI 网页应用收款 | `https://aipay.alipay.com/open-flow/products` | 3 张网站截图：**首页 / 商品页 / 支付页**。支付页必须展示支付宝付款方式并处于等待付款状态，**不是支付成功页**（`onboarding/flow.md:324`）。先查签约状态，已签则无需截图 |
| AI 按量付费 | `https://aipay.alipay.com/open-flow/products?product=metered` | 无截图。签约后还需在**服务市场注册**取得真实 `serviceId`，替换 `alipay-aipay-build/.env.example` 里的占位 `api_mock_service_id` |
| Token Pay / AI 订阅 | 企业账号：`https://b.alipay.com/page/product-workspace/solution-detail?solutionCode=SP000001000000101144`<br>个人账号：`https://aipay.alipay.com/merchant-intent`（联系销售） | 先确认 app `2021006170668597` 归属哪个账号；若挂在个人账号下，须在企业账号下新建应用并重配密钥 |

**B. 即刻收（白名单，链长）**

| 步骤 | 动作 | 入口 |
|---|---|---|
| 1 | 提交创作者留资申请白名单 | `https://render.alipay.com/p/yuyan/180022670000179729/index.html?type=creator&source=aipay` |
| 2 | 手机支付宝搜索小程序「**即刻收**」，完成认证与签约 | 支付宝 App |
| 3 | 在小程序创建商品，选收费模式（次数包 `QUANTITY`+`COUNT` / 积分包 `QUANTITY`+`POINT`）与档位价格，等待商品可售 | 同上 |
| 4 | 商品详情页复制**商品 ID**（`np_d_` 前缀），作为 `out_product_id` | 同上 |
| 5 | 为现有应用 `2021006170668597` 申请即刻收直连接口权限 | 支付宝开放平台 |

B 组步骤 1、5 是审批项，2-4 是手工配置项，全部无脚本通道。

## 选定第一款后的实现完整度审计

**2026-10-11 用户拍板：只用第一款（AI 网页应用收款）。** 该产品收款主链路已在生产验证，故剩余工作量不在开通，而在实现完整度。

官方要求的 API（从 webpay 指南全文提取，`alipay.trade.*` 去重后共 5 个）+ 技能 `integration/flow.md:18` 的六项要求，逐项对账 new-api：

| 官方要求 | new-api | 位置 |
|---|---|---|
| 下单 `alipay.trade.page.pay` | ✅ | `controller/subscription_payment_alipay.go:82-89`（PC，`FAST_INSTANT_TRADE_PAY`） |
| 手机网站 `alipay.trade.wap.pay` | ✅ 额外 | 同文件 `:73-80`（`QUICK_WAP_WAY`），UA 判移动端见 `:63-67` |
| 查询 `alipay.trade.query` | ✅ | `controller/agent_topup.go:184-186`、`controller/topup_backfill.go:40-42` |
| 退款 `alipay.trade.refund` | ❌ **缺** | 全仓 0 引用 |
| 退款查询 `alipay.trade.fastpay.refund.query` | ❌ **缺** | 全仓 0 引用 |
| 关闭 `alipay.trade.close` | ❌ **缺** | 全仓 0 引用 |
| 异步通知 + 验签 | ✅ | `controller/topup_alipay.go:100-103`、`subscription_payment_alipay.go:151-154`；`controller/payment_safety_gates_test.go` 三个测试锁死「`VerifySign` 失败必须 write `fail` 并在 `GetTopUpByTradeNo` 之前早退」 |
| 同步回跳 `return_url` | ✅ | `subscription_payment_alipay.go:75`、`:84` |

技能 `integration/flow.md:307` 的 `[INT.CODE.INTERFACES_COMPLETE]` 要求「网站支付/APP 支付覆盖下单、交易查询、退款、退款查询、关闭交易和异步通知处理代码」——**不满足，缺 3 项**。但这 3 项均不影响收款主链路（24 笔生产成功即证），影响的是**退款与对账**。

### 缺口一：pending 单永久悬挂（这解释了生产那 55 笔）

> 状态：支付宝侧一半已修（见下文「重要修正」与尾巴第 1 项），本地一半未修（尾巴第 2 项）。

下单未设 `timeout_express`（全仓 0 命中 `TimeoutExpress`/`TimeExpire`），也没有 `alipay.trade.close`。支付宝侧订单永不过期，我方也永不关闭 → `top_ups.status=pending` 只增不减。

风险不是资金损失（notify 仍会按 `GetTopUpByTradeNo` 找到单并入账），而是：

- 「55 笔 pending」不反映任何真实状态，无法用作对账口径
- 用户可能拿几天前的旧链接付款，钱确实到账，但运营侧完全无预期
- 无法主动作废一张发错的单

### 缺口二：退款是人工工单，不是 API

`controller/agent_ability.go:99` `AgentAbilityRefundApply` → `service/agent_ability.go:277` `AgentAdminHandleRefund` → `model/agent_refund.go:128` `HandleAgentRefundRequest`。末端实现只做一次 UPDATE：

```go
Updates(map[string]interface{}{
    "status":          status,
    "handle_time":     time.Now().Unix(),
    "handle_admin_id": adminId,
    "admin_remark":    strings.TrimSpace(remark),
}).Error
```

**不动 `users.quota`、不调支付宝退款接口、不更新 `top_ups`。** 是个纯工单状态机，实际退款靠运营手工去商家后台点。

后果：后台退完钱，系统内额度不会自动扣回，账实不符；且无 `alipay.trade.fastpay.refund.query`，退款是否真到账我方无从核验。

注：该工单流挂在 `agent_ability` 路由下，语义是「智能体能力退款」，与充值单退款并非同一域——即便补齐 API，也需先决定退款入口归谁。

## SDK 能力核查：缺口全部是「未调用」而非「未实现」

`go.mod:65` → `github.com/smartwalle/alipay/v3 v3.2.29`。逐项核该版本：

| 能力 | SDK 位置 | 签名 |
|---|---|---|
| `TimeoutExpress` 字段 | `trade_type.go:38`（`Trade` 内嵌结构体） | 「该笔订单允许的最晚付款时间，**逾期将关闭交易**。取值范围 1m～15d。m-分钟，h-小时，d-天，1c-当天。不接受小数点」 |
| `TimeExpire` 字段 | `trade_type.go:39` | 绝对超时，`yyyy-MM-dd HH:mm:ss` |
| 退款 | `trade.go:44` | `TradeRefund(ctx, param) (*TradeRefundRsp, error)` |
| 退款查询 | `trade.go:20` | `TradeFastPayRefundQuery(ctx, param) (*TradeFastPayRefundQueryRsp, error)` |
| 关闭交易 | `trade.go:32` | `TradeClose(ctx, param) (*TradeCloseRsp, error)` |
| 撤销交易 | `trade.go:38` | `TradeCancel(ctx, param) (*TradeCancelRsp, error)` |
| 异步退款 | `trade.go:80` | `TradeRefundAsync(...)` |
| 下单（PC） | `trade.go:10` | `TradePagePay(param) (result *url.URL, err error)` |
| 下单（JSAPI，我们不用） | `trade.go:62` | `TradeCreate(ctx, param)` |

`TradePagePay` 与 `TradeWapPay`（`wap_type.go:4`）**都内嵌 `Trade`**，故 `TimeoutExpress` 对两条分支同样可用，无需分别处理。

**结论：三个「缺失」接口零依赖新增，纯调用问题。**

### 重要修正：`TimeoutExpress` 只关支付宝那一侧，不清本地 pending

`TimeoutExpress` 的官方语义是「逾期将**关闭交易**」——支付宝侧自动关单，`alipay.trade.close` 因此降为可选（仅「主动作废一张发错的单」才需要）。

但**改完复读代码，推翻了此前「只需这一个字段即可闭合缺口一」的判断**：

- 异步通知 handler（`subscription_payment_alipay.go:264-268`）对 `trade_status` 非 `TRADE_SUCCESS`/`TRADE_FINISHED` 的请求**一律写 "fail" 早退**。即便支付宝真为超时关闭的单发来 `TRADE_CLOSED`，我方也不会改写本地状态，反而会触发支付宝 8 次重试。
- 全仓**没有** pending 单的时间驱动清扫：`ExpireSubscriptionOrder` / `UpdatePendingTopUpStatus` 的调用点全是「下单失败」或 Stripe webhook，无一处按时间触发。`topup_backfill.go:35` 的 `alipay.trade.query` 只服务「已成功但缺渠道交易号」的回填，`rsp.TradeStatus` 非成功即返错，根本不碰 pending。

故 `TimeoutExpress` 的真实收益是**堵住「用户几天后付一张我们早已当废单的订单」这个口子**，让支付宝侧状态可预期；**不是**让后台那 55 笔消失。要让本地 pending 收敛，还需一件独立的事：用 `alipay.trade.query` 把 `TRADE_CLOSED` 的单标 `expired`（查单函数已就绪，可挂在 backfill 那个管理员触发接口上）。

未验证支付宝是否会为「未付款即超时关闭」的交易发送异步通知——官方只承诺付款成功类通知。上述清扫方案主动查单，不依赖这一点。

### 沙箱实测：`timeout_express` 确被服务端解析（2026-10-11）

用用户提供的沙箱凭据（app `9021000169624526`，网关 `openapi-sandbox.dl.alipaydev.com`；凭据落 `tmp/alipay-sandbox.env`，已被 `.gitignore:39` 覆盖）跑一次性探针对 `alipay.trade.precreate` 下单，**用非法值做证伪**：

| `timeout_express` | 结果 |
| --- | --- |
| `30m`（生产将用的值） | 下单成功 |
| `1m`（下界） | 下单成功 |
| `15d`（上界） | 下单成功 |
| `1.5h` | 被拒 `ACQ.INVALID_PARAMETER`／「参数无效：订单支付超时时间**格式错误**」 |
| `20d` | 被拒 `ACQ.INVALID_PARAMETER`／「参数无效：订单支付超时时间**范围不支持**」 |
| `abc` | 被拒 `ACQ.INVALID_PARAMETER`／「格式错误」 |

非法值被针对性拒绝、且错误文案点名「订单支付超时时间」，证明该字段**被服务端真实解析并按 1m~15d 校验**，不是被静默忽略。`30m` 是有效值。

**仍未能实测「逾期自动关单」这一行为本身**，原因见下条——不是没试，是试不出来。

### 实测障碍：未被打开/扫码的单在支付宝侧不可查

探针第一版同时下两张单（一张 `1m`、一张不设时限）做对照组，等 95 秒后查单，**两组都返回 `ACQ.TRADE_NOT_EXIST`**——包括已经拿到二维码的那张。

即 `alipay.trade.precreate` 返回 `qr_code` 成功，但交易记录要等买家实际扫码才落地；`alipay.trade.page.pay` 同理（`BuildURL` 纯本地拼串，买家不打开收银台就没有交易）。所以「设时限的单被关闭、不设的仍开着」这个对照**无从观察**，自动关单只能采信官方字段语义「逾期将关闭交易」。

**第二版实验（用生产函数 + 主动打开收银台）仍未能观察，已停手。** 做法：临时把 `alipayOrderTimeout` 改为 `1m`，用生产函数 `alipayWebPayURL` 生成 A 组链接、手工构造无时限的 B 组作对照，再用 HTTP GET 打开两个收银台链接试图让交易落地，等 100 秒后复查。结果：

- A 组（`FAST_INSTANT_TRADE_PAY` + `timeout_express=1m`）GET 返回 **HTTP 500**，B 组返回 **HTTP 404**；
- 两组查单在 t=0 与 t=100s 均为 `ACQ.TRADE_NOT_EXIST`；
- 实验前置断言都通过（A 组链接确含 `timeout_express`、B 组确不含），失败点在「让交易落地」这一步。

对照：同一 A 组链接用 `curl -L` 打开时返回 **HTTP 200 并重定向到 `excashier-sandbox.dl.alipaydev.com/standard/auth.htm?payOrderId=<id>`**——即**网关确实受理了这张单并签发了 payOrderId**，页面 64KB 是沙箱收银台 SPA（页面里的 `error` 字样只是打包后的 JS 日志桩 `error:function(){}`，非报错）。

结论：**交易要等买家登录/提交支付才落地**，脚本拿不到浏览器会话与沙箱买家账号，因此「逾期自动关单」这一行为**在本轮无法用脚本证明**。三次尝试（precreate 对照、page.pay + GET、curl 打开）均止步于同一处，按「修三轮还坏就查产品前提」停手，不再换姿势重试。

**要真正跑通付款那一步，唯一路径是人工**：用沙箱买家账号在浏览器里打开收银台链接完成支付。这需要沙箱买家账号（用户侧资料，本轮未取得）。

**这条反过来修正了尾巴第 2 项（pending 清扫）的设计**：清扫不能只认 `TRADE_CLOSED`。用户从未打开过的单查单会一直返回 `ACQ.TRADE_NOT_EXIST`，必须把「查不到 + 下单时间已超过时限」也判为 `expired`，否则那 55 笔里的大多数根本清不掉。

### `alipay-aipay` 技能驱动不了 savvy：不支持 Go

本轮按用户要求走了 `alipay-aipay` 技能（v1.7.0，`integration_only` + `--product-type webpay`），在第一个业务动作 `resolve-project` 即被拒：

```
INTEGRATION_MESSAGE_ERROR:该路径未识别到项目根标记，不能确认是服务端项目根
候选：new-api\electron（package.json）；new-api\web（package.json）；
     new-api\web\classic（package.json）；new-api\web\default（package.json）
```

两条独立证据确认这是**语言支持缺口**而非路径写错：

1. 该 runner 接受的项目根标记为 `package.json`、`pom.xml`、`build.gradle`、`pyproject.toml`、`requirements.txt`、`composer.json`、`*.csproj`、`*.sln`——**没有 `go.mod`**。
2. 全技能目录 `grep -ro "go\.mod" references/` **0 命中**；`integration/flow.md:117` 的新建项目默认框架固定为 Java/Spring Boot、PHP/Laravel、Python/FastAPI、C#/ASP.NET Core、Node.js/Express，同样无 Go。

它给出的四个候选全是前端目录，选任何一个都与「服务端支付代码」不符，故未选。技能的登记恢复路径（换候选/给更精确路径/新建项目）没有「语言不支持」分支，无法在本项目内继续。

**影响**：技能的沙箱准备、代码生成、联调与收口流程都用不到 savvy 上；本轮的沙箱验证是绕过技能、直接用 Go SDK 打沙箱网关完成的。若日后要用技能自带的沙箱与 demo，只能新建一个 Node.js/Express 之类的受支持项目，产物与 savvy 的 Go 实现无法共用。

### 顺带核出：用户提供的 Java 示例填错了公钥

用户给的 `new DefaultAlipayClient(gateway, APP_ID, PRIVATE_KEY, "json", "UTF-8", AIPAY_PUBLIC_KEY, "RSA2")` 第 6 参是**支付宝公钥**位，填的却是「沙箱应用公钥」。比对密钥内容可证：`AIPAY_PUBLIC_KEY` 的模数（`yGC+fPRb…`）与 `AIPAY_PRIVATE_PKCS_KEY` 一致，是应用私钥的公钥对；支付宝公钥是 `AIPAY_ALIPAY_PUBLIC_KEY`（模数 `hUTNzlNB…`）。填错的症状是下单能成、**验签全败**，回调一律被拒。

Go 侧不受影响：`subscription_payment_alipay.go` 的公钥来自 `operation_setting.AlipayPublicKey`，与私钥是两个独立配置项。

### Skill Pay 层不需要新接口

`skillpay.md:34` 要求取 `alipay.trade.page.pay` 返回的 `pageRedirectionData` 字段，再喂给 `alipay-bot trigger-payment-signal --payment-link "<cashier_url>"`。

`pageRedirectionData` 在 Go SDK v3.2.29 中 **0 命中**——但 SDK 的 `TradePagePay` 直接返回 `*url.URL`（`trade.go:10`），即收银台跳转 URL 本身。该 URL 就是文档所说的「过长需要压缩」的那条 `cashier_url`，取 `result.String()` 即可作为 `--payment-link` 入参。**无需改造下单调用，也无需换 SDK。**

## 限制

- 白名单审批结果与时效未知；留资表单无查询接口。
- 未验证栗橙（个体工商户）是否在即刻收直连的准入主体范围内——`nowpay-intro.md:55-58` 只写「支持支付宝个人账号、支付宝企业账号」，未单独说明个体工商户。
- **app `2021006170668597` 的账号归属未核**（个人/个体工商户 vs 企业主体）。Token Pay 已确认仅企业账号可一站式接入，故该项直接决定 Token Pay 能否复用现有密钥对。
- 未抓取 `api-list/` 下六个 API 的字段级文档（`charge_query` / `purchase_consult` / `purchase_create` / `quota_query` / `quota_verify` / `quota_refresh`），真要开发时需逐份读。
- 抓取期间 `aipay.alipay.com` 出现**间歇性 TLS 握手失败**（curl exit 35，schannel）：同一时刻 `/callpay.md` 返回 200，而几分钟前刚成功的 `/llms.txt` 返回 000。非路径特异性，重试即可，但意味着单次抓取失败不能当作「文档不存在」的证据。
- `/docs/subscribe/introduce.md` 始终未能取得（多次重试均 exit 35）；Token Pay 的准入结论取自产品页 `subscription.md` 与主索引的「注意」行，未读到接入指南正文。
- **`timeout_express` 的实测全部在沙箱**（app `9021000169624526`）。已证服务端解析并校验该字段，但「逾期自动关单」这一行为本身**未直接观察到**——原因是未被打开/扫码的单在支付宝侧不可查（见上文「实测障碍」）。生产是否同此行为、以及沙箱与生产的关单时效是否一致，均未验证。
- 沙箱网关本身不稳：同一次探针运行中，第一次 B 组下单返回 HTML 而非 JSON（`invalid character '<'`），重跑即成功。沙箱下单失败不可直接当作参数问题。
- **未取得沙箱买家账号**，因此无法在浏览器里完成一次真实沙箱支付；付款、异步通知、返跳这三段全部未经端到端验证。这是「本地先验证」目前唯一的硬缺口。
- `alipay-aipay` 技能 v1.7.0 不支持 Go（见上文），故本轮未经过技能自带的沙箱准备、字段校验与收口检查；技能侧的「沙箱测试通过」判定本轮无法取得。

## 尾巴

**选定第一款后，按优先级：**

1. ~~**补 `TimeoutExpress`**~~ **已落地（2026-10-11，用户拍板 30 分钟）**。`subscription_payment_alipay.go` 新增 `const alipayOrderTimeout = "30m"`，`alipayWebPayURL` 的 WAP 与 Page 两分支各设一行。测试 `TestAlipayWebPayURL_SetsOrderTimeout` 锁两条分支，已做反向验证（摘掉一行即变红，报错里 `biz_content` 确实不含 `timeout_express`）。**注意其收益边界见上文「重要修正」——只关支付宝侧，不清本地 pending。**
2. **本地 pending 清扫（新增，缺口一真正的收口）**。用 `alipay.trade.query` 把死单标 `expired`。查单函数 `alipayQueryOrderFn`（`topup_backfill.go:35`）已存在但只认成功态，需放宽；无定时任务，可先挂管理员触发接口。**判据不能只认 `TRADE_CLOSED`**——实测未被打开/扫码的单一直返回 `ACQ.TRADE_NOT_EXIST`，必须把「查不到 + 下单时间已超过时限」一并判死，否则大多数 pending 清不掉。
3. **`alipay.trade.close`（可选）**。仅用于主动作废发错的单——`TimeoutExpress` 已覆盖过期场景。SDK 现成（`trade.go:32`）。
4. **补 `alipay.trade.refund` + `alipay.trade.fastpay.refund.query`**（SDK `trade.go:44` / `:20`，零依赖新增）。前置决策：退款入口归谁——现有 `agent_ability` 工单流语义是「智能体能力退款」，与充值单退款不同域，不能直接复用。且退款成功必须同事务扣回 `users.quota`，否则账实不符。
5. **Skill Pay 技能层**（若仍要「发布一个 agent 应用接支付宝 AI 收」）。底座已就绪且返回的 `*url.URL` 可直接当 `--payment-link`，工作量在写「商家下单 Skill」本身 + `alipay-bot` 两条命令编排，不在支付接口。官方 Node.js demo：`github.com/alipay/ai/tree/main/code_example/page-pay-demo-with-agentpay`。
6. **核实 app `2021006170668597` 的签约产品列表**，确认 salesCode 是否为 `I1080300001000041203`。闭合方式见上文「但有一个未闭合的缺口」。

**已降级（本轮不选）：**

7. `alipay-aipay-build/`（A2M 完整实现，288 行 `server.js`）**整个目录未入版本控制**。选了第一款即暂不用它；若日后启用需生产化改造：网关 `openapi-sandbox.dl.alipaydev.com` → `openapi.alipay.com`、`ALIPAY_SERVICE_ID=api_mock_service_id` → 服务市场注册的真实 serviceId、订单存储从 JSON 文件 → DB。
8. 即刻收若真接，savvy 的 token 计费口径要先决定怎么映射成整数积分——先决设计题，不是接线题。
9. `deploy/ops/ops_check_alipay.sh` 未提交。
10. **本轮改动未部署**：只落了代码与测试，机B 生产仍跑旧镜像。上生产需走 `git push origin dev && git push gitlab dev` → 机B `git pull --ff-only` → `docker compose build new-api && up -d new-api`。分支 `feat/alipay-aipay-onboarding` 尚未合回 `dev`。
11. **扫码下单两条路径未动**（`topup_alipay.go:121`、`subscription_payment_alipay.go:230`，均为 `FACE_TO_FACE_PAYMENT`）。代码注释称二维码支付宝侧默认 2 小时有效，故未强加 30m 时限——那是缩短一条已在跑通的路径的有效期，属行为变更，需另行决定。这两条路径的本地 pending 同样不清扫，归入第 2 项。
