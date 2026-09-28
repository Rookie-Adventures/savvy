# 摘除路径 A 后模型仍发 AI 授权链接：中毒会话上下文里的幻觉单

## 症状
充值 widget 在手机浏览器（非微信）里仍出现"微信 AI 支付授权"卡（`payapp.weixin.qq.com` 链接）。
点开后的报错从 09-28 的「请勿使用他人的支付链接」变成了「繁忙」。用户没有开通 H5 收款产品，
所以第一反应是"是不是又走到 X402/H5 了"。

## 结论：与 H5 权限无关，路径 A 确实已经断了，那张卡是模型自己编的
`deploy/ops/diag_zeroclaw_tools.sh` 实测（机B，时间一律 +0800，zeroclaw 重启在 01:36:24）：

| 证据 | 读数 |
|---|---|
| `/api/skill/invoke` | 重启后 0 次；末次 01:30:09 |
| weixinpay-mcp `tools/call` | 重启后 0 次；末次 01:30:11 |
| 渲染后的 config.toml | `allowed_tools` 只剩 8 个 `savvy__*` |
| 01:40:06 / 01:41:15 / 01:48:32 三轮回复 | trace 里 **`tool_calls: 0`**，正文却带 payapp 链接 |

编造的痕迹（拿真单对照即可判定）：
- 三条 `sid` 各不相同、且只在自己那一条里出现。真 `sid` 来自 402 响应的 `payment_code`，不调工具拿不到。
- 同一条回复顺手编了 `out_trade_no=WX402_20260929013011RtYkLmNpQwZx`：库里那个时段没有任何 WX402 单，
  后缀 12 位也不符真单格式（`WX402_` + 14 位时间 + 5 位随机，如 `WX402_20260928145154AiqG`）。

报错文案为什么变：
- 09-28 那条是**真单**，绑的是服务端共享 weixinpay 插件的设备身份（machine-id 唯一绑定人），
  别的微信用户打开 → 「请勿使用他人的支付链接」。
- 09-29 这三条是**假 sid**，微信侧无从判归属，只能给通用「繁忙」。
- 也就是说"他人链接"消失不代表问题变好，是链接从真变假了。

## 根因
zeroclaw 的会话落盘在 `data/zeroclaw/.zeroclaw/data/sessions/sessions.db`，前端把 `session_id` 存
localStorage 续多轮（`agent-chat/index.tsx:51`）。配置/IDENTITY 改了、容器重启了，**旧会话历史没清**，
里面还留着重启前真实 X402 回复的形状。DeepSeek 在被要求"充值"时直接模仿历史，连工具都不调。
`allowed_tools` 管得住能不能执行，管不住模型抄自家旧回复。

## 本次改动
1. `deploy/zeroclaw/config.toml.tpl`：weixinpay 的 `[[mcp.servers]]` 整块注释掉，
   `[mcp_bundles.topup].servers` 收成 `["savvy"]` —— 上次只摘 `allowed_tools`，工具形状仍进 prompt。
2. `deploy/zeroclaw/IDENTITY.md`：加硬规则——支付/授权链接只能来自本轮工具返回值，
   禁止照抄历史拼 URL，禁止输出 `payapp.weixin.qq.com`；没有工具返回就如实说下单失败。
3. 前端兜底 `agent-chat/lib/pay-links.ts` + `index.tsx`：`extractPayLinks` 不再认 payapp 为支付卡，
   新增 `stripAgentAuthLinks()` 对所有 assistant 消息无条件剥掉授权链接（含被剥空的 markdown 链接）。
   本宿主不提供路径 A，这类链接出现即幻觉，不该留可点入口。
4. `deploy/ops/purge_zeroclaw_sessions.sh`（一次性，不并入部署脚本）：停容器 → 会话库改名留档
   `*.poisoned-<ts>.db`（可回滚）→ 起容器，强制新会话。
5. `deploy/ops/diag_zeroclaw_tools.sh`：本次取证探针保留，下次同类问题直接跑。

## 验证
- 前端：`bun run typecheck` 中 agent-chat 零报错（仓库存量错误在 auth/api.ts、billing-history-dialog，与本次无关）；
  `bun run build` 通过；一次性脚本跑真函数——幻觉文本 → 支付卡 `[]`、展示文本不含 payapp；
  真路径 B 文本 → `weixin://` 卡与 claim_url 行为不变。
- 部署后实测（机B 03:04 清洗会话 → 03:06 新会话真跑一轮 `充值 0.1 元`）：
  - 生效 config：weixinpay `[[mcp.servers]]` 为注释态，`servers = ["savvy"]`；
  - 回复含 `weixin://wxpay/bizpayurl?pr=…` 与真 claim_url，**不含 payapp**；
  - 该单确实落库：`WXAGT20260929030613s1Z0… | wechat_agent | user_id=0 | pending | 0.1`（grounded，不是编的）；
  - 清洗后 weixinpay `tools/call` = 0、`/api/skill/invoke` = 0；trace `iteration: 2`（先调工具再作答）。

## 限制 / 尾巴
- 聊天历史被一次性清空（充值 widget 的一次性上下文，判定可弃）。留档 `sessions.poisoned-20260929030448.db` 可回滚。
  旧浏览器 localStorage 里失效的 `session_id` 实测不会报错（用一个不存在的 id 发一轮 → HTTP 200 + 正常新建会话），
  所以清洗对用户是"接着能用"，不需要前端配合。
- weixinpay-mcp 容器仍在跑、绑卡状态保留，只是不再挂进自家 agent 的 prompt；X402 服务端能力
  （`/api/skill/invoke` 等）原样保留给第三方自带插件的 agent。
- 模型仍可能编出别的假话（如"已第 5 次下单"）。本轮只堵"假付款入口"这一条最伤钱的。
- 新会话那轮回复开头冒了英文 `Order created ✅` 再接中文——IDENTITY 的语言规则没完全压住，待观察。
- 未修的小缺陷：旧卡片上「微信里付款」按钮对已不可付的单仍然可点，2-4ms 内失败。
