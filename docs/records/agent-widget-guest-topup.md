# 全站悬浮智能体 + 免登录"先付后认领"充值 落地记录

## 现象/目标
智易收签约表单要求"智能体访问地址"为我方托管页面（支付宝客服确认），原 `/agent-chat` 在登录墙后审核员打不开；产品侧同时要新访客零门槛"对话→充值"。目标：入口改全站悬浮 widget、游客免登录可聊天可支付、支付后经认领卡片登录/注册自动入账，智能体全程不碰账户体系。

## 方案
- 设计：`docs/superpowers/specs/2026-09-02-agent-widget-guest-topup-design.md`
- 计划：`docs/superpowers/plans/2026-09-02-agent-widget-guest-topup.md`
- 分支：`feature/agent-widget-guest-topup`（基于 feature/agent-chat-payment）
- 核心决策：先付后认领（否决"智能体代注册+随机密码"——密码进百炼对话历史、LLM 持注册接口=injection 面、对话报账户名充错账无法抗辩）；复用 topups 表+现有 /api/user/alipay/notify 回调栈（否决新建 pending_topups 表）；认领凭据用 crypto/rand claim_token（outTradeNo 由 LLM 生成可枚举，不能当凭据）。

## 改动清单
后端（Task 1-3，commits af0168e64e..9224a5af09）：
- `model/topup.go`：TopUp 新增 ClaimToken 列（varchar64 索引，AutoMigrate）、provider 常量 `alipay_agent`、GetTopUpByClaimToken（含空串防护）
- `controller/agent_topup.go`（新）：agentQuotaAmountFromMoney（getPayMoney 逆运算，Price=0 兜底）、newClaimToken（crypto/rand hex32）、游客 IP 限额（复用 common.InMemoryRateLimiter，hour/day 双窗，OptionMap 键 AgentGuestChatHourLimit=10/AgentGuestChatDayLimit=50）、RegisterAgentTopUp、AgentTopUpStatus（pending>10s 按需 TradeQuery 兜底）、agentClaimDecision 纯函数、ClaimAgentTopUp（锁内重取+幂等+防抢认领）
- `controller/agent_chat.go`：游客（id==0）走 allowGuestChat
- `controller/topup_alipay.go`：AlipayNotify 增 alipay_agent 分支（金额以回调 total_amount 为准；游客单只标记等认领）+ completeAgentTopUp 共用完成逻辑
- `router/api-router.go`：/agent/chat 迁 userRoute+TryUserAuth；新增 /agent/topup/register（TryUserAuth）、/agent/topup/status（匿名，token 即凭据）、/agent/topup/claim（selfRoute）

前端（Task 4-5，commits d219f08e3e..c6f403115f）：
- `features/agent-chat/widget.tsx`（新）：右下角悬浮按钮+聊天浮窗，挂 `routes/__root.tsx` 全站可见，显隐沿用模块开关 chat.agent_chat；open 时从 sessionStorage 恢复未认领单渲染横幅
- 删除：`routes/_authenticated/agent-chat/`、侧边栏导航项（use-sidebar-data.ts）、URL_TO_CONFIG_MAP 条目
- `lib/agent-order.ts`（新）：从支付链接 biz_content 解析 outTradeNo/totalAmount
- `lib/claim-storage.ts`（新）：sessionStorage 认领凭据（agent_topup_claims）
- `components/claim-card.tsx`（新）：waiting/paid/credited 三态，5s 容错轮询，已登录自动认领，未登录跳 /sign-in?redirect= 或 /sign-up
- `components/payment-card.tsx`：挂载即登记换 token，Go to Pay 后渲染 ClaimCard，协议勾选门禁零改动
- i18n：5 新键 + 1 键改文案 × en/zh/ja/fr/ru/vi（手改，**勿跑 npm run i18n:sync——会把历史待翻队列的无关改动注入 locale**）

## 验证
- go build ./... 成功；go test ./controller/... ./model/... ./setting/... 全 ok（本特性新增 TestAgentQuotaAmountFromMoney/TestNewClaimToken/TestGuestChatLimiter/TestAgentClaimDecision 全 PASS，Phase 1 存量 5 项 PASS）
- npm run typecheck 0 error；npm run build 成功；locale 键完整性脚本 6/6 OK
- 每 Task 经 CodeReview 子代理审查：T1 Approved-with-fixes（已修 I-1 空 token/M-1 除零）、T2 Approved（已修 I-1 限流器换 InMemoryRateLimiter/M-1 nil 兜底/M-4 clientIP）、T3 Approved clean（资金安全专项：notify/claim/查单三方竞态在 LockOrder 下闭环，无双重加额路径）、T4 Needs-fixes→已修（6 语言键同步+aria-label i18n）、T5 Needs-fixes→已修（轮询容错+登记静默）
- 全分支终审：Needs-fixes→已修 I-1（status 接口 claimed 语义加支付门槛，登录单未付款不再误显"已到账"）；遗留 Minor 分诊均可留（M-2 float 往返 2 位小数无损/M-3 TOCTOU 唯一索引兜底/dev 模式 devtools 重叠仅开发期）；其余端到端链路/前后端契约/越权面/回归面全部 ✅

## 限制
- 游客限额内存计数，重启清零（防刷定位，可接受）
- Update 成功后 IncreaseUserQuota 失败的 latent money leak 与现有 AlipayNotify 同构保留；客服兜底口径：topups 表 status=success 且 user_id>0 但无入账日志的按 Money 人工补
- 金额换算不套 AmountDiscount（预设档位促销与任意金额订单不适用）
- TOKENS 展示类型的换算分支无单测（无公开 setter），集成验收时覆盖
- claim_token 丢失（付完关浏览器）→ 钱留 pending，凭支付宝账单订单号客服人工认领
- 极小额订单换算额度为 0 时不入账、认领被拒（客服按 Money 处理）

## 尾巴
- 部署（管理员）：option API 写 AgentBailianHost/AppId/Key（**轮换泄露过的旧 Key**）+ 重启容器；百炼智能体提示词更新（支付后引导"在下方卡片登录或注册领取充值"、禁问手机/电脑）并重新发布
- 动工前置验证项：V0 站点 AlipayAppId==MCP 应用 APPID（回调验签/查单兜底前提）；V2 百炼正式版 MCP 可否配 AP_NOTIFY_URL=https://<域名>/api/user/alipay/notify（可配则回调直推，状态接口查单兜底仍在）
- 智易收签约通过后：切 create-alipay-payment-agent（收款单），支付卡片扩展链接+二维码双渲染（复用 wallet QRCodeSVG）
- 0.01 元真单手工验收 8 步清单见计划 Task 6 Step 4，验收结果补录本文档
- temp/alipay-qr-topup 分支遗留（stash + 未跟踪的 manual-alipay-topup.tsx/alipay-qr.png/temp docs）确认废弃后清理
