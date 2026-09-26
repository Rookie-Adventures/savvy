# 服务号对话框原生支付集成方案（设计 + 实施计划）

> 目标：让用户关注「栗橙科技」服务号后，直接在对话框里说话就能用 AI、要充值时在**微信内原生调起支付**（不跳网页、不扫二维码、不依赖 weixinpay 插件）。
> 本文件为设计基线；代码脚手架见 `new-api/service/wechat_mp.go` 与 `new-api/controller/wechat_mp.go`（本回合**未注册路由、未部署**）。
>
> **架构修订（2026-09-26 22:xx）**：原方案把服务号每条消息都丢给 **Hermes agent** 当大脑，实测 token 成本不可接受（重型 agent 每消息重载完整 system context + 工具定义）。已改为 **零 token 确定性意图路由**（`controller/wechat_mp.go` 的 `routeMpIntent`），覆盖 savvy-quota-topup 的 6 个有限意图；账户类意图在 openid 绑定（wechat-identity-phase1）上线后直连能力层，同样无需 LLM。`service/wechat_mp.go` 已删除 Hermes 调用。

---

## 1. 现状（代码实证，2026-09-26 核对）

已就绪、可复用：
- `POST /api/user/agent/chat`（`controller/agent_chat.go`）→ 仍绑 **Bailian**（与「去百炼」方向已不符，不作为本方案大脑）。
- `StreamHermesMessage`（`controller/hermes.go`）+ `service.CallHermesAgentStream`（`service/hermes.go`）→ 直连 `HERMES_AGENT_URL`（`/v1/chat/completions`，OpenAI 兼容 SSE）。**原拟作为服务号大脑,但 2026-09-26 因 token 成本否决——服务号改用零 token 确定性路由,不再调用此 agent**。
- `POST /api/user/agent/wechat/topup/create`（`controller/agent_wechat_topup.go`）→ 仅返回 Native `code_url`（扫码）。**不是**服务号内原生调起。
- JSAPI 收银台（`subscription_payment_wechat.go: SubscriptionRequestWechatJsapi`）+ 微信身份体系（phase1）已测绿、未部署。
- 支付配置：`operation_setting` 已有 `WechatMpAppId` / `WechatAppSecret`（JSAPI OAuth 用），经 `common.OptionMap` 持久化（`model/option.go:128-129`）。

**缺口（本方案需新建）：**
1. 服务号**消息接收/验签网关**（GET 握手 + POST 收消息）——全仓无。
2. **客服消息下发客户端**（`cgi-bin/message/custom/send`）——5 秒被动回复超时，必须异步回推，全仓无。
3. **agent 触发的 JSAPI 充值端点**（带 openid，返回 JSAPI 调起参数）——现有 JSAPI 端点绑 web session openid，不接服务号消息流。
4. 配置缺口：`WechatMpToken`（服务号消息回调验签 Token，后台可配）——`operation_setting` 缺失。
5. **H5 支付页**（前端）：服务号内 `WeixinJSBridge.invoke` 必须在网页里执行，客服消息文本无法跑 JS；需一个极简 H5 页，拿到订单后自动调起 JSAPI 支付 sheet。

---

## 2. 目标架构

```
用户微信 ──发消息──▶ 服务号
服务号 ──XML POST──▶ new-api /api/user/wechat/mp/message   （新：消息网关, 匿名+签名校验）
                          │  ① 即时被动回复 "正在处理…" (5s 内返回, 避免微信重试)
                          ├─② 异步 goroutine:
                          │     a. routeMpIntent 确定性意图路由(零 token, 覆盖 6 个有限意图):
                          │        - 含「充值/买额度」+ 金额 → CreateAgentMpJsapiTopUp(openid, 金额)
                          │            → JSAPI 预下单(用 openid) → 返回 pay_url
                          │            → 客服消息(图文) 推送 pay_url
                          │        - 查余额/用量/订单/兑换/退款 → 账户类意图,
                          │            v1 给确定性引导(等 openid 绑定后直连能力层)
                          │        - 其他 → 菜单话术
                          ▼
用户微信 ──点图文/链接──▶ H5 支付页(/mp/pay?token=…) 在微信内打开
                          → WeixinJSBridge.invoke('getBrandWCPayRequest', 调起参数)
                          → 原生支付 sheet → 付款 → /api/user/wechat/notify 入账
```

要点：
- **不跳网页 ≠ 不打开页**：服务号内「原生支付 sheet」必须由网页里的 `WeixinJSBridge` 触发，故用一条客服消息(图文)把用户带进 H5 支付页，页内自动弹 sheet。这是微信官方 JSAPI 在对话场景的标准姿势，体验上仍是「对话框里点一下就付」，区别于现有 Native 扫码。
- **不调用任何重型 agent（Hermes / Bailian）**：6 个意图有限且可枚举，用纯规则路由 `routeMpIntent` 即可零 token 命中；重型 agent 每消息重载完整 system context + 工具定义，token 成本不可接受。账户类意图在 openid 绑定上线后直连能力层，同样无需 LLM。
- **openid 来源**：服务号消息 XML 的 `FromUserName` 即用户 openid，无需再走 OAuth（身份体系 phase1 的 OAuth 是给 web 用的）。

---

## 3. 新增/改动模块

| 模块 | 文件 | 说明 |
|---|---|---|
| 消息原语 | `new-api/service/wechat_mp.go` | `VerifyMpSignature` / `getMpAccessToken`(缓存) / `SendCustomTextMessage` / `SendCustomNewsMessage` / `ParseInboundMpMessage`(XML) |
| 消息网关 | `new-api/controller/wechat_mp.go` | `WechatMpMessageGet`(握手echo) / `WechatMpMessagePost`(验签→解析→即时被动回复→异步 agent+客服消息) |
| agent JSAPI 充值 | `new-api/controller/wechat_mp.go` 或 `agent_wechat_topup.go` | `CreateAgentMpJsapiTopUp(openid, amount)`：建单 + `GetWechatJsapiClient().PrepayWithRequestPayment` + 返回 pay_url |
| 配置 | `new-api/setting/operation_setting/payment_wechat.go` | 新增 `WechatMpToken` |
| 配置持久化 | `new-api/model/option.go` | `common.OptionMap["WechatMpToken"]` 注册（save 侧）；load 侧补回读 |
| 路由 | `new-api/router/api-router.go` | 匿名 GET/POST `/wechat/mp/message`（签名即鉴权，对齐 `WechatJsapiOauthCallback` 匿名范式）|
| H5 支付页 | `new-api/web/default/src/...` | 极简页：凭 token 取订单 → `WeixinJSBridge.invoke` 调起；**前端任务，本回合仅留接口约定** |

---

## 4. 关键设计决策

1. **服务号网关不调用任何重型 agent（Hermes / Bailian）**。savvy-quota-topup 已把意图收敛为 6 个有限、可枚举动作（充值/余额/用量/订单/兑换/退款），用纯规则路由 `routeMpIntent` 即可零 token 命中，避免重型 agent 每消息重载上下文导致 token 成本爆炸。账户类意图在 openid 绑定（wechat-identity-phase1）上线后直连 `agent_ability.go` 能力层，同样无需 LLM。
2. **JSAPI 而非 Native**：服务号内原生体验靠 JSAPI（`WeixinJSBridge`），复用 `GetWechatJsapiClient()` + `jsapi.PrepayRequest{Payer.Openid}`（字段实证：`Appid/TimeStamp/NonceStr/Package/SignType/PaySign`）。Native `code_url` 仅作降级保留。
3. **客服消息异步**：POST 入口 5 秒内必须返回被动回复（"正在处理…"），真正推理与支付推送放 goroutine，通过 `cgi-bin/message/custom/send` 回推。需 `access_token` 缓存（微信限频，7200s 过期）。
4. **金额由服务端裁定**：与 `savvy-quota-topup` SKILL 约定一致——**不在客户端/技能写死金额范围**，复用 `agentTopUpAmountCents`（0.01~5000 元，`math.Round` 防截断）。
5. **安全**：消息网关匿名，鉴权靠 `WechatMpToken` 签名校验（SHA1 排序拼接）；客服消息与下单走服务端，openid 来自微信消息体，不暴露给用户。

---

## 5. 实施分步

**P0（本回合脚手架，未上线）**
- [x] 设计文档（本文件）
- [x] `service/wechat_mp.go`：验签 / token 缓存 / 客服消息 / XML 解析 / Hermes 非流式调用
- [x] `WechatMpToken` 配置 + OptionMap 注册
- [x] `controller/wechat_mp.go`：消息网关 + `CreateAgentMpJsapiTopUp`
- [ ] 路由注册（**门禁：需你确认后执行**）
- [ ] `go build ./...` / `go test` 编译自检（**门禁**）

**P1（上线前，需你拍板）**
- [ ] 服务号后台：填 `WechatMpToken`、配回调域名 `https://scheng.net/api/user/wechat/mp/message`、开「客服消息」接口权限、已认证服务号 + 商户号 + JSAPI 支付授权目录
- [ ] compose 透传 `HERMES_AGENT_URL`（及既有 `SAVVY_HMAC_SECRET` 等）
- [ ] 灰度：先单用户/白名单验证消息收发 + 一笔 0.1 元 JSAPI 充值入账

**P2（体验闭环）**
- [ ] H5 支付页（前端）：`/mp/pay?token=` → 取 JSAPI 调起参数 → `WeixinJSBridge.invoke` → 支付成功回跳
- [ ] agent 意图识别精化（金额抽取、含糊意图反问）
- [ ] 失败兜底：JSAPI 不可用时降级 Native `code_url` 图文
- [ ] 限流：按 openid 维度（规避已知 IP 共桶 bug，内存 P0）

---

## 6. 风险与回滚

- **P0 风险**：`gin` 重名路由导致启动 panic（历史已踩过，见 `7807753776`）→ 路由注册后用 `TestSetApiRouterHasNoDuplicateRoutes` 守卫。
- **P1 风险**：服务号未认证/未开客服消息权限 → 消息能收但不能回推；上线前先确认接口权限。
- **回滚**：消息网关为新增匿名路由，不改动既有 `agent/chat` / `topup/create`；出问题只需摘掉该路由 + 停服务号回调即可，不影响现有充值链路。

---

## 7. 本回合已落地内容（未部署）

- `docs/skillpay/wechat-mp-native-payment-integration.md`（本文件）
- `new-api/service/wechat_mp.go`（新增）
- `new-api/controller/wechat_mp.go`（新增）
- **修订（22:xx）**：`service/wechat_mp.go` 已删除 `CallHermesAgent`；`controller/wechat_mp.go` 改为 `routeMpIntent` 零 token 确定性路由（Hermes 方案因 token 成本否决）。
- `new-api/setting/operation_setting/payment_wechat.go`：新增 `WechatMpToken`
- `new-api/model/option.go`：`WechatMpToken` 注册进 OptionMap（save 侧）
- **未做**：路由注册、部署、服务号后台配置、H5 支付页、编译自检
