# X402 付款人 openid 与用户绑定 openid 不同 AppID 空间（直入账结构性空转）

日期：2026-09-28
状态：**已定性，未修**（不阻塞收款；已把错误假设从代码与文档中钉掉）

## 症状

1. 用户在第二台手机（另一个微信账号）打开 widget 给出的支付链接，微信报「无法支付：请勿使用他人的支付链接」。
2. 同日晚些时候上线的「付款人 openid 直入账（老客户零点击）」在生产上不会命中任何用户。

## 根因

两条独立事实叠加：

**其一，部署形态与 X402 的设计前提不符。**
微信 AI 付（X402 / `AUTH_AND_PAY`）的官方模型是：插件装在**用户自己的智能体客户端**里（SkillHub FAQ 原文：「weixinpay 插件默认在最新版本的 WorkBuddy / QClaw 中已安装，商户改造无需关心」），因此"设备身份 == 当前用户"天然成立。我们把 `weixinpay-mcp` 作为**全站共享单容器**部署，设备身份 `device_user_id` 由 `/etc/machine-id` 派生并持久化（`weixinpay-mcp/Dockerfile:7`、`entrypoint.sh:5-7`），于是全站只有一个绑定人，产出的 payapp 授权链接归属该账号，其他微信用户打开即被微信判为"他人链接"。

**其二，openid 按 AppID 隔离，我们两侧用了不同 AppID。**
商户号绑了两个 AppID：Native 老号 `WechatAppId` 与服务号 `WechatMpAppId`（`setting/operation_setting/payment_wechat.go:6-9` 注释「Native 继续用老号，勿混」）。

- 扫码登录绑 **服务号 AppID** → `users.wechat_id` 存的是服务号空间 openid；
- X402 走 **Native 老号** 下单（`controller/skill_invoke.go:227` → `createSkillPayNativeOrder`，`pay_data.type` 硬传 `code_url`）→ 查单/回调的 `payer.openid` 是老号空间。

两空间 openid 是不同字符串、永不相等，所以 `model.GetUserIdByWeChatNativeOpenid` 在 X402 路径上结构性不可能命中。该函数原注释却断言「payer.openid 与该列同源(下单即用 WechatAppId)」——这是一个未经验证的假设，正是本次误判的来源。

## 佐证与核查

- 生产 `users` 表：`wechat_id` 非空 0 条、`mp_openid` 非空 0 条 —— 当前根本没有任何用户绑定过微信身份，即使空间一致也无从匹配。
- 历史微信充值单 5 笔、去重后 3 个付款人 openid，命中 `wechat_id` 0、命中 `mp_openid` 0。
- 3 个 openid 前缀分两组（`o7kFs3VS…` 与 `oqnnN3A…/oqnnN3H…`），与"两套 AppID 各产一套 openid"一致。**注**：openid 前缀一致性不是微信官方保证的规则，此条仅作旁证，不作结论依据。
- 官方文档核实（2026-09-28 读原文）：AI 专属卡属于**用户**；「笔笔确认模式」要求**用户本人扫码进微信 Liteapp 输密码授权**；`POST /aipay/preorder` 的请求/应答中**没有任何付款人字段**（只有 `pay_mode`、`expires_at`，应答仅 `payment_code`）→ 付款人由"谁来扫码授权"决定。微信侧**不存在**"一个 agent 实例只能一个付款人"的限制。

## 本次改动

不改支付行为（改了也不会命中，且钱路径不宜空跑）。只把错误假设钉掉：

1. `new-api/model/user.go` `GetUserIdByWeChatNativeOpenid` 注释重写：明确前提尚未成立、X402 路径结构性不命中、指向本文与 AGENTS.md。
2. `AGENTS.md` 铁律三新增「openid 空间铁律」段，并更正 §4 第 3 条——`/mp/pay` 落 SPA 兜底页的问题早在 `eb4e922602`（同日）已修（现生成 `/api/mp/pay?token=`），记录过期。

## 影响面

**不是资金事故。** 未命中的单全部回落游客认领，而认领链当天已加固：建单即预生成 `claim_token` 并随 402 下发 `claim_url`、新增匿名 `GET /api/skill/recover?claim_token=`、主节点 5 分钟兜底扫描补履约、超 Native 有效期 2h 自动关单。实测近 6 小时 7 笔 X402 单 `payer_openid` 全空、无一扣款。

## 待办 / 尾巴

1. ~~**商户平台配置**：JSAPI 支付授权目录需追加 `https://scheng.net/api/`~~ **作废（同日查官方规则后撤回）**：官方「配置JSAPI支付授权目录」明确——**只配置到域名**（如 `https://scheng.net/`）时「只校验实际支付页面协议(https/http)和域名是否与配置的一致，**不校验域名后面的多级目录**」。我们商户号现有配置正是域名形式，**已覆盖 `/api/mp/pay` 等全部子路径，无需追加**。残余注意项：域名大小写敏感、必须以 `/` 结尾。另：授权目录配在**商户号**上，与 AppID 无关；AppID 的要求是"下单 appid 与 openid 同号"，属另一件事。
2. **要让"对话内直入账"成真，二选一**（尚未决策）：
   - ① widget 在微信内**绕开 X402**，复用已有服务号 JSAPI 链路（`CreateAgentMpJsapiTopUp` → `/api/mp/pay`）。付款人=当前微信用户，天然同号，无新协议风险。**倾向此路。**
   - ② 真把 X402 接 JSAPI（`createJsapiOrder` + `pay_data.type='prepay_id'`）。**未验证前提**：SkillHub 预下单是否接受服务号 AppID 产生的 prepay_id（官方 FAQ 未涉及跨 AppID 限制）。若做，按铁律三 Go 侧与 `wechatpay-direct-v3` SDK 侧须同步改（SDK 现硬编码 `createNativeOrder`）。
3. **共享服务端 weixinpay 插件的定位**需明确：它对"客户自带 agent 客户端"的交付形态有效，对自家 widget 无效；是否从 zeroclaw `allowed_tools` 摘除待 ①/② 决策后定。
4. 用户微信身份绑定链（扫码登录是否真在生产使用、`WeChatServerAddress` 代理配的是哪个 AppID）需独立核实——两列全 0 说明该链可能整体未被使用。
