# 微信身份体系第一阶段落地记录（服务号 OAuth 绑定 + 扫码登录）

## 现象/目标
已登录用户可扫码绑定微信；桌面端可扫码用微信登录（未绑定时[创建新账户]/[绑定已有账户]两分支）；
微信内浏览器可直接授权登录；老 `users.wechat_id` 路径与其并存、零改动；未来接开放平台/小程序不需重构。

## 方案
- 独立 `wechat_accounts` 表存 (provider, app_id, openid)→user_id，微信只是第三方 identity provider，
  `users/user_id` 永远是核心；两把唯一索引（openid 唯一、user+provider+app 唯一）+ 创建前查占用，
  **绝不覆盖既有绑定**。
- 一次性 `wechat_oauth_tokens` 表承载跨设备流程。桌面与手机不共享 session，故 state 采用
  「kind:token」自含票证：token 为 crypto/rand 32 字节 hex（256bit），不可猜测即天然防 CSRF；
  5 分钟过期；条件更新（RowsAffected=0 即失败）保证单用性由 DB 兜底。
- OAuth 换码复用 Task 9 的 `service.ExchangeWechatOauthCode`；配置复用 Task 9 的
  `WechatMpAppId/WechatAppSecret`（零新增配置项）；登录落地复用 setupLogin 的 session 字段集合。

### state=kind:token 状态机
```
            CreateWeChatOAuthToken(kind)
                      │ pending(5min)
      ┌───────────────┼──────────────────────┐
      │ bind          │ login                │ direct(微信内)
      ▼               ▼                      ▼
  openid 已占用?   openid 已绑?           openid 已绑?
  Y: rejected      Y: completed(带uid)    Y: consumed + 手机setupLogin → 302 /console/topup
  N: completed     N: authorized          N: authorized → 302 /sign-in?wx_token=
      │               │                      │(未绑回跳)
      ▼               ▼                      ▼
  (绑定完成)      claim mode=login:       claim mode=create: 建用户+绑定+会话落地(初始密码仅此一次明文)
                  completed→consumed      或 bind-existing(selfRoute): authorized→consumed
```

### 跨设备时序（桌面扫码登录）
```
桌面                          服务端                          手机微信
 │ POST /api/wechat/oa/tokens │
 │◄───{token, url}────────────│
 │ [QR=url, 2s 轮询 status]    │
 │◄───────────────────────────│◄── GET /api/wechat/oa/entry?state=login:token ──┐
 │                            │◄── GET /api/wechat/oa/callback?code&state ──────┘
 │                            │  换 openid → authorized/completed
 │ 轮询到 authorized/completed │
 │ claim(login|create)        │
 │◄── 会话/账户 ───────────────│
```

## 改动清单
- model/wechat_account.go、model/wechat_oauth_token.go（新建，含显式 TableName 规避 gorm
  WeChatAccount→we_chat_accounts 的默认命名）、model/main.go（双迁移清单注册）
- service/wechat_oauth.go：BuildWechatOauthAuthorizeURL 重构为 BuildWechatOauthAuthorizeURLFor
  薄 wrapper（Task 9 行为零变化，其单测仍绿）
- controller/wechat_oa_identity.go：tokens(login/direct/bind)/entry/callback/status/claim/
  claim-existing/binding(GET|DELETE) 八个 handler；换码抽包级变量便于测试 stub
- router/api-router.go：匿名 5 条 + selfRoute 4 条
- 前端：lib/wechat-ua.ts（isWechatInAppBrowser 自 wallet 移入，wallet re-export 零变化）；
  auth api +3、profile api +4；sign-in wechat-qr-login-dialog（QR+轮询+两按钮+初始密码展示）；
  user-auth-form（扫码按钮、微信内 direct 自动跳授权、?wx_token= 处理、登录后自动认领绑定）；
  profile wechat-bind-dialog + wechat-bind-card；i18n ×6（24 键）

## 验证
- `go test ./model/... ./controller/...` 全绿；`go build ./...` 通过；`npx tsc --noEmit` 与
  `npm run build` 通过。service 包仅存 Task 9 时代已核实的 2 个 channel-affinity 存量失败
  （来自更早合并，与本分支无关）。
- 测试覆盖：票证单用/过期拒绝、openid 占用拒绝覆盖、bind rejected、login 未绑 authorized、
  direct 已绑会话落地 302、direct 未绑回跳、claim create 建户+消费一次、claim login 二次拒绝、
  bind-existing 占用拒绝、解绑往返、bind 票证归属 403。
- (生产验收：认证+域名就绪后——桌面发起绑定→手机扫码→轮询成功；桌面扫码登录未绑两分支各走一遍；
  微信内打开登录页直登。本任务未部署、未动生产 DB。)

## 限制
- unionid 暂空（服务号 snsapi_base 不返回），仅 openid。
- 老 `users.wechat_id` 路径（controller/wechat.go + wechat-server 代理）原样并存，两套身份不互通。
- direct 已绑落地页硬编码 `/console/topup`；callback 错误为极简中文文本页。
- claim create 受 `common.RegisterEnabled` 开关约束（对齐老微信注册路径）。
- 绑定票证轮询走匿名 status 端点（票证即权限，bind kind 在 handler 内校验归属）。

## 尾巴
- unionid 回填（开放平台绑定后）；绑定管理后台可视化；模板消息通知。
- callback 错误路径可改为 302 到前端错误页（当前极简文本页）。
- 一次扫码流程串行消耗 tokens/status/claim 多个 Critical 限流配额，高频重试可能触发 429。
