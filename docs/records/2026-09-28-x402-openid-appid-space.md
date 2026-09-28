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
2. ~~**要让"对话内直入账"成真，二选一（尚未决策）**~~ **已定案（2026-09-28 P1/P2 探针后）**：
   - **① 采纳**：widget 在微信内**绕开 X402**，复用已有服务号 JSAPI 链路（`CreateAgentMpJsapiTopUp` → `/api/mp/pay`）。付款人=当前微信用户，天然同号，无未验证前提。
   - **② 否决**：X402 接 JSAPI 在协议层可行（见 P1），但 **payment_code 的兑换只存在于受支持宿主的插件内部**（见 P2），自家网页无法自助兑换 → 走不通。`/api/skill/invoke` 与 X402 原样保留，服务第三方 agent。
3. **共享服务端 weixinpay 插件的定位**需明确：它对"客户自带 agent 客户端"的交付形态有效，对自家 widget 无效；是否从 zeroclaw `allowed_tools` 摘除待 ① 落地后定。
4. 用户微信身份绑定链（扫码登录是否真在生产使用、`WeChatServerAddress` 代理配的是哪个 AppID）需独立核实——两列全 0 说明该链可能整体未被使用。**① 落地时这条必须一并解决**：JSAPI 下单要有当前用户的服务号 openid。

## P1 / P2 探针实测（含复现方法，免得下次重探）

**P1 — SkillHub X402 预下单接受哪种 `pay_data.type`？结论：不拒 `prepay_id`，且不校验 value 真实性。**
用官方 SDK 直接调预下单，两组都填**假值**：

```
对照组 code_url(假值)  => 接受, 拿到 payment_code(长度 36)
P1  prepay_id (假值)   => 接受, 拿到 payment_code(长度 36)
```

推论：预下单只是"把传入字符串封装+签一张名"，**不向微信支付侧核单**。所以 `payment_code` **不能当作"该订单真实存在"的证据**；真正的校验发生在用户授权那一环。目前不构成漏洞（只有我们自己调），但写方案时别把它当凭证。

**P2 — 我们自己的页面能不能兑换 payment_code？结论：不能。**
拿假码直调官方 `weixinpay_pay`，返回 **「此模式暂不支持微信AI支付，请到电脑端使用」** —— 在校验支付码**之前**就被宿主/会话模式门禁拦下。工具签名亦注明 `agentSessionId` 由宿主自动注入"当前 CodeBuddy 会话 ID"。故兑换路径绑在受支持宿主 + 其会话/设备上，商户侧无可调用端点。

**复现要点（省下次踩坑）**：
- 配置来源是 **`/opt/savvy/deploy/.env`**，不是 options 表（options 表里 `skillpay`/`x402` 键数为 0）。键名：`SKILLPAY_DEVELOPER_ID` / `SKILLPAY_PUB_KEY_ID` / `SKILLPAY_SKILL_ID` / `SKILLPAY_SKILL_VERSION` / `SKILLPAY_PRIVATE_KEY_PATH`。
- `SKILLPAY_PRIVATE_KEY_PATH` 存的是**容器内路径** `/secrets/skillpay-private.pem`，宿主机真身在 `/opt/savvy/secrets/skillpay-private.pem`。
- **机B 宿主机没有 node**，要跑 Node 脚本得 `docker cp` 进 `weixinpay-mcp` 容器内执行（用完删掉临时文件）。
- SDK 零依赖（`node>=14`），入口 `wechatpay-direct-v3/lib/x402-pay.js` 导出 `X402Preorder`；构造必填四项：`developerId / pubKeyId / privateKeyPath / skillId`（漏 `skillId` 会报 `CONFIG_MISSING`）；调法 `preorder(value, transport, payType)`，`payType ∈ {code_url, prepay_id, h5_url}`。
- 官方插件包 `tenpay-weixinpay-ai-installer` 的 `dist/cli.mjs` 带 `/*__WECHATPAY_JS_ARMORED__*/` 字符串保护，`grep http` / base64 扫描都抽不到端点，别在这上面耗时间。
- 插件日志 `deploy/data/weixinpay/plugin-data/weixinpay/logs/*.xlog` 是 **mars 加密格式**（文件头 `07 01 00 06`），`strings` 与 zlib 解帧均拿不到 URL，无密钥不可读。

