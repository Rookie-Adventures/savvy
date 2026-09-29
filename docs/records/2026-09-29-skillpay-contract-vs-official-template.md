# 对照 SkillHub 官方改造模板，修正我们 Pay Skill 的契约与身份配置

## 症状
2026-09-29 复盘"AI 授权卡"时发现：我们的 X402 实现是自己拼出来的，从没跟官方口径对过。
拿到社区元技能 `@user_4894573e/skill-paid` v3.1.1（SkillHub「一键改造付费技能」）逐项对照后，
确认有三处会让**第三方 agent 付费后取不到货**，另有一处把凭据当配置用了。

## 根因 / 错配点（官方证据 → 我们的实现）

| # | 官方口径 | 我们原来 | 后果 |
|---|---|---|---|
| 1 | `SKILL_ID` = 已发布的 **slug**（`config_collection_checklist.md:27`）；`bt_` 前缀是 **SkillHub Token**，标 🔴敏感、「仅用于 skillhub CLI 发布/升级，绝不写入 Skill 包」（`:45`） | 生产 `SKILLPAY_SKILL_ID=bt_n2c…` 被写进 L2 `skill_info.skill_id`（`service/skillpay_x402.go`）参与签名并发往预下单端点 | 发布凭据当业务配置外发；平台按 slug 对账时身份不对 |
| 2 | 场景二（支付后重试）**只带 `X-Out-Trade-No`**（`scripts/templates_builtin.py` handle_invoke:773-791），`WeixinPay-Required` 属"可一并携带" | 无该头即 401 `PAYMENT_CODE_INVALID` | 照模板生成的 agent 付了钱取不到货（最硬的一条） |
| 3 | 402 body 除嵌套 `WeixinPay` 块外，**顶层再冗余 `WeixinPay-Required` + `prompt`**（`templates_builtin.py:370-386`） | 只有嵌套块 | 只读 body 顶层的 agent 根本识别不到支付码 |
| 4 | `amount` 为**整数、单位「分」**（`checklist:29`） | `"%.2f"` 元字符串 | agent 侧展示/对账差 100 倍 |
| 5 | `expires_at` 留余量，模板取 14 分钟（上限 900s） | 用满 900s | 签名时间戳与平台时钟稍有偏差即判过期 |
| 6 | frontmatter 需 `triggers` 数组、`pricing`、`author`（`publish_guide.md:32-41`、通过率建议 `:57`） | 触发词埋在 description 长段落，无 triggers/author/pricing | 审核按"描述与能力不符/金额配置不一致"驳回的常见项 |

## 本次改动
1. `setting/operation_setting/payment_skillpay.go`：装载 env 时**拒收凭据形状**的 `skill_id`
   （`bt_`/`sh-`/`PUB_KEY_` 前缀 → 置空）。宁可 `/api/skill/invoke` 回 503，也不带凭据去签名外发。
2. `controller/skill_invoke.go` `handleSkillPayRetry`：付款码从"门票"降级为"免查单凭证"——
   无码不再拒，但幂等缓存只对**码校过**或**订单已标记 paid** 开放，其余一律实查渠道，由微信 `trade_state` 裁定。
   安全不减（拿到订单号 ≠ 付过钱），兼容第三方 agent。
3. 同文件 `respondSkillPay402`：补顶层 `WeixinPay-Required` + `prompt`（与嵌套块同源同值）；
   `amount` 改「分」整数，给人看的金额仍留在 `message` 的 ¥ 文案里。
4. `service/skillpay_x402.go`：`expires_at` 改 `now+14*60`。
5. `skillhub-packages/savvy-quota-topup/SKILL.md`：2.1.1 → **2.2.0**，补 `triggers` 数组与 `author`，
   changelog 记录以上服务端契约变更；zip 重打包（LF、单条 `SKILL.md`、与真源字节一致），
   两份副本（`~/.workbuddy/skills/`、`.qoder/skills/`）同步复制。

## 验证
- `go build ./...` 通过；`go vet` 干净。
- `go test ./controller/ ./setting/operation_setting/`：新增用例全绿
  - `TestSkillPayRetryWithoutPaymentCode` 四个子用例：无码+未付 → 实查渠道并 402、**不吐缓存**；
    无码+渠道 SUCCESS → 200 履约并回写 `transaction_id`；码错 → 仍 401 且不查单；
    无码但订单已 paid → 走幂等缓存不多打一次查单。
  - `TestSkillPaySkillIdRejectsCredentialShape`：`bt_/sh-/PUB_KEY_` 被识别，`savvy-quota-topup` 不误判；
    env 填 Token 时装载即置空且 `IsSkillPayConfigured()` 为 false。
  - 402 双通道断言并入 `skill_pay_recovery_test.go`（顶层键、嵌套一致、amount=10 分、message 含 ¥0.10）。
- `go test ./service/` 有一个**存量**失败 `TestObserveChannelAffinityUsageCacheByRelayFormat_*`，
  与本次无关（把 `skillpay_x402.go` 单独 stash 后仍红）。

## 影响面 / 已知取舍
- 部署后生产 `SKILLPAY_SKILL_ID` 仍是 `bt_…` → `/api/skill/invoke` 与 `/api/skill/recover` **立刻 503**。
  这是有意的安全状态：在 slug 核实并替换前，路径 A 不该对外可用。
  自家充值不受影响——widget 走路径 B（`/api/user/agent/wechat/topup/create`），不经这道门槛；
  X402 单的后台 sweep 停摆也只影响提醒推送。
- 无码重试每次多打一次微信查单，属预期成本。

### 机B 部署实测（0474c4ad0，21:18 +0800 重启 new-api/zeroclaw）
- bundle 内 `SKILL.md` = `version: 2.2.0`；
- `POST /api/skill/invoke` → `503 SKILLPAY_DISABLED`（守门生效，也证明跑的是新二进制）；
- 新会话对话「我要充值 0.1 元」→ 回复带真 `weixin://wxpay/bizpayurl?pr=…` 与认领链接，**不含 payapp**，
  同时落库 `WXAGT20260929212316… | wechat_agent | user_id=0 | pending` → 路径 B 完好；
- 待办回收动作：后台取到真 slug 后改 `deploy/.env` 的 `SKILLPAY_SKILL_ID` 并重启 new-api，路径 A 才会恢复。

## 待办（需要人去后台拿/做的事，见本文件末节清单）
- `SKILLPAY_SKILL_ID` 换成后台确认的 slug；`SKILLPAY_SKILL_VERSION` 与发布版本对齐（现在钉 1.0.0，技能 2.2.0）。
- `bt_` Token 轮换（它已在我们容器 env 和签名串里待过）。
- `product_id` 目前每次随机（官方要求固定且与后台定价一致）——等后台确认商品口径再改。
- SkillHub 的 `pricing` 只给了 `mode: per_call` + 固定 `price`；我们是"用户自定金额充值"，
  这一形态平台是否支持，必须先问清再往 frontmatter 里填数，不能编一个价。

## 去哪里拿这些（速查）
| 要拿的东西 | 在哪 | 备注 |
|---|---|---|
| 已发布的 **slug** | SkillHub 后台 → 我的 Skill | `publish_guide.md:25`：提交后不可修改，生成器 `SKILL_ID` 必须与之一致 |
| **发布版本号** | 同上（技能详情） | 填进 `SKILLPAY_SKILL_VERSION` |
| **SkillHub Token**（`bt_`） | 商户中心 → 开发者密钥 / Token 页 | 🔴 只给 `skillhub` CLI 发布用，**只展示有限次**；用于轮换，不进包、不进 skill_id |
| **Developer ID**（`sh-`）、**公钥 ID**（`PUB_KEY_`+32位大写HEX）、公钥 PEM | 商户中心 → 开发者密钥 | 这三样是公开项，可写入包/配置 |
| **私钥 PEM** | 同上下载 | 我们这边权威位置 `secrets/skillhub_dev_key_9078.pem`，只经 `SKILLPAY_PRIVATE_KEY(_PATH)` 注入 |
| **定价（分）与 product_id** | 商户中心 → 定价 / 商品 | `AMOUNT`、`PRODUCT_ID` 必须与后台一致，否则审核按"金额配置不一致"驳回 |
| 支付产品形态（`code_url`/`prepay_id`/`h5_url`） | 微信商户平台 → 产品权限 | 对应 `PAY_DATA_TYPE`；我们没有 H5 权限 |
| 上架流程原文 | `https://skillhub.cn/tutorials#agent-pay-publish` | 元技能里的 `publish_guide.md` 是其精简版 |
