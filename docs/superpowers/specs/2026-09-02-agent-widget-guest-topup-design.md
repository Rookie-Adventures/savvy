# 全站悬浮智能体 + 免登录"先付后认领"充值 设计

日期：2026-09-02
状态：已与需求方逐节确认
前置分支：`feature/agent-chat-payment`（Phase 1 聊天页/后端转发/支付卡片，未合并）
实施分支：基于 `feature/agent-chat-payment` 开新特性分支

## 1. 背景与目标

- 智易收签约表单要求提供"智能体访问地址"，支付宝客服确认该地址即我方托管的页面 → 页面必须公网可访问且审核员能体验完整收款流程。
- 现有 `/agent-chat` 是登录后才可见的独立页面，审核员打不开；同时产品侧希望新访客零门槛体验"对话→充值"。
- 目标：①入口改为全站右下角悬浮 widget；②游客免登录可聊天、可支付；③支付成功后通过"前端认领卡片"把钱记到账户（登录或注册），智能体全程不碰账户体系。

## 2. 已确认的关键决策

| 决策点 | 结论 | 否决项及理由 |
|---|---|---|
| 免登录动机 | 过签约审核 + 新客转化，两者都要 | — |
| 账户流 | 方案 B：先付款，后由前端认领卡片完成登录/注册，自动入账 | 方案 A（智能体代注册+随机密码）：密码明文进百炼对话历史；LLM 持注册接口 = prompt injection 攻击面；对话报账户名易充错账 |
| 入口形态 | 全站悬浮 widget，删除 `/agent-chat` 独立路由与侧边栏入口 | 保留双入口：多一处维护面，用户明确要省事 |
| 认领凭据 | 后端随机 `claim_token`（128-bit），只发给下单浏览器会话 | 直接用 outTradeNo 认领：订单号由 LLM 生成、可枚举，会被抢认领 |
| 金额来源 | 以支付宝回调/查询结果为准，不信对话内容 | — |

## 3. 架构总览

```
游客/用户 → 全站悬浮 widget（聊天浮窗）
   → POST /api/user/agent/chat（TryUserAuth 游客可用，IP 限流）
   → 百炼智能体（仅挂 AI 支付 MCP，内置工具/自定义工具全关）
   → 回复含 alipay 链接 → 前端剥离原文、渲染支付卡片（协议勾选门禁）
   → 前端解析 out_trade_no → POST 登记接口 → 得 claim_token（sessionStorage），
     后端落现有 topups 表（provider=alipay_agent，游客 user_id=0）
   → 用户支付 → 到账确认（复用 /api/user/alipay/notify 回调 + 按需 TradeQuery 兜底）
     → topups 记录标记 success，金额以支付宝侧 total_amount 为准
   → 游客：聊天窗弹认领卡片 [登录][注册] → 跳转 sign-in/register 页（带 redirect 回跳）
     → 回跳后 widget 从 sessionStorage 恢复认领态 → POST 认领接口（登录态 + claim_token）
     → 额度入账 → 卡片变"已到账"
   → 登录用户：登记时已绑 user_id，到账确认后自动入账，无需认领
```

## 4. 前端改动

1. **悬浮 widget 组件**：右下角圆形图标 + 聊天浮窗；复用 `features/agent-chat` 的聊天组件、`pay-links.ts` 链接剥离、PaymentCard 协议勾选（游客同样必须勾选《用户协议》《隐私政策》，每笔独立重置）。
2. **删除**：`/agent-chat` 路由（`routes/_authenticated/agent-chat`）、侧边栏入口及 `use-sidebar-config` 注册；原模块开关语义改为"控制 widget 显隐"。
3. **认领卡片组件**：支付确认前隐藏；确认后出现在聊天流中。站点的登录/注册是**整页路由**（`(auth)/sign-in`、register，支持 `?redirect=` 回跳），不是弹窗——认领卡片按钮跳转登录/注册页并带 redirect 回当前页；claim_token + outTradeNo 存 sessionStorage，回跳后 widget 挂载时扫描恢复，已登录则自动认领；未登录展示 [登录][注册] 按钮与"已到账"终态。
4. **登记调用**：从支付链接 `biz_content` 解析 `out_trade_no` 后调登记接口拿 claim_token；解析失败则不显示认领卡片（仅普通支付卡片），走客服兜底。
5. i18n 六语言（en/ja/fr/ru/vi/zh）同步全部新文案。

## 5. 后端改动（new-api，Go）

分层照项目规矩：`router/ → controller/ → service/ → model/`；JSON 一律走 `common/json.go`；DB 三库兼容（SQLite/MySQL/PG）。

1. **聊天接口游客通道**：`/api/user/agent/chat` 从 selfRoute 改为 `middleware.TryUserAuth()`（已存在的可选鉴权中间件，middleware/auth.go:169），无登录态可调；游客按 IP 限流（默认 10 条/小时、50 条/日，经 OptionMap 可调）；登录用户沿用现有配额。
2. **登记接口**（TryUserAuth + 限流）：入参 outTradeNo；登录用户绑定 user_id，游客 user_id=0；生成 claim_token（crypto/rand 128-bit）；**落现有 `topups` 表**（provider=`alipay_agent`，status=pending）。同一 outTradeNo 重复登记返回错误，前端以 sessionStorage 里首次登记拿到的 token 为准。
3. **认领接口**（需登录态，限流）：入参 claim_token；校验记录存在、provider=alipay_agent、已支付、未认领（user_id=0）；绑定当前用户，额度换算用 getPayMoney 的逆运算（实付 RMB ÷ Price ÷ 分组倍率），入账走现有 `IncreaseUserQuota` + `RecordTopupLog` + 蚂蚁链存证；幂等（本人重复认领返回成功，他人拒绝）。
4. **到账确认（全复用现有栈，不建新通道）**：①回调——MCP 订单与站点直连订单同 APPID，`AP_NOTIFY_URL` 指向现有 `/api/user/alipay/notify`，在 `AlipayNotify` 内按 provider=alipay_agent 分支：金额以回调 `total_amount` 为准回填 Money，user_id>0 直接入账，user_id=0 只标记已支付等认领；②兜底——前端状态查询接口触发按需 `cli.TradeQuery(out_trade_no)`（现有 GetAlipayClient），查到已付即走同一完成逻辑，无需 cron。回调不可用时（百炼未开放 AP_NOTIFY_URL）兜底通道独立成立。
5. **不建新表**：复用 `model.TopUp`（topups 表，GORM 自动迁移），新增 `ClaimToken` 列（varchar(64)，索引）与新 provider 常量 `alipay_agent`；游客单 user_id=0、Amount=0（认领/入账时按实付回填）。过期策略：30 天未认领由现有清理逻辑标 expired（不删数据，支持客服人工处理）。
6. **客服兜底**：不做后台页面（YAGNI）；人工凭支付宝账单订单号在 DB 侧核对后手工认领，流程写入 `docs/records/`。

## 6. 智能体侧（百炼）

- 仅挂"AI 支付"MCP；**内置工具（bash/write/read/edit/glob/grep/download_file）全部关闭**；自定义工具/知识库/数据连接器不加（LLM 零账户权限是红线）。
- 短期记忆轮数保持默认量级（后端已有 session_id 多轮）。
- 提示词：支付链接给出后引导"支付完成后请在下方卡片登录或注册领取充值"；禁止询问"手机还是电脑"。
- 改动后必须重新**发布**才生效。
- 智易收签约通过后：切换 `create-alipay-payment-agent`（收款单，agentName 品牌露出），前端支付卡片扩展"链接+二维码"双渲染（复用 wallet QRCodeSVG）。

## 7. 动工前验证项（阻塞项）

0. **同应用验证**：站点现有支付宝配置的 AlipayAppId 必须与 MCP 受限密钥应用（栗橙网络科技 2021006170668597）为同一 APPID——这是回调验签复用和 TradeQuery 兜底成立的前提；若不同，需管理员将站点支付配置切到同一应用，否则 §5.4 两通道全部失效需回炉。
1. 百炼正式版"支付宝"MCP 配置是否支持 `AP_NOTIFY_URL`（决定回调通道成立与否；不成立则仅靠 §5.4② 按需查单，功能不受损、到账感知变懒）。
2. 受限密钥工具勾选状态（已勾全部 5 个）+ 产品生效状态（昨日 invalid-open-scene-api-permission 排障结论）。

## 8. 安全与风控

- 游客聊天 = 匿名烧 token 面：IP 限流 + 每日上限；上线后观察，必要时加轻量人机验证（预留，不实现）。
- claim_token 防枚举；认领必须登录态；入账幂等。
- 支付卡片协议勾选门禁保留（项目既有合规规范）。
- 百炼 API Key 仍仅存服务端 OptionMap，前端零密钥；部署时轮换曾泄露的旧 Key（Key 值不写入任何文档）。

## 9. 测试计划

- 单测（Go）：claim_token 生成/校验、认领幂等、游客限流、金额以到账确认为准、pending 状态机。
- 前端：typecheck + build；widget 在营销页/登录页两种布局下不遮挡关键操作。
- 手工验收（0.01 元真单）：游客全链路（聊天→建单→支付→注册→认领到账）；登录用户直充链路；token 丢失走客服兜底演练一次。

## 10. 明确不做（YAGNI）

- 智能体代注册/代查账户/数据库工具
- 认领管理后台页面
- 流式输出（沿用 Phase 1 非流式，后续另议）
