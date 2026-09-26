# 转交 Qcode：百炼 / 机B 智能体接入微信收款

> 用途：把"智能体如何收款（用户充 Savvy 额度）"这件事，交给正在用百炼 CLI 配置的 Qcode 拍板/接手。
> 背景：我们后端在 `scheng.net`，已就绪：微信 Native 扫码下单 + X402 卡片支付两条链路。

---

## 一、已核实、别再走的弯路（重要，先读）

1. **微信支付官方 `weixinpay` 插件（对话内 AI 收银台卡片）不可移植。**
   它是 WorkBuddy 专属的「MCP 服务（`dist/mcp-server.mjs`）+ 原生 CLI 二进制（`prebuilds/.../WeChatPayCLI.app`，约 19MB）+ 客户端卡片 UI」三件套。
   - 百炼只认 **HTTP/OpenAPI** 自定义插件，且**没有对话内支付卡片的渲染层** → 传包/装安装器都跑不起来。
   - 机B 的 new-api 也不是 MCP 宿主 → 同样装不了。
2. **百炼上只能走「路径 B：生成微信二维码让用户扫码」**，没有"点一下卡片"的体验。
3. **机B 的 new-api 前端已自带完整收银台**：`features/wallet/` 钱包页 + 微信 JSAPI/Native（直连我们自己的微信商户号）+ `topup/create` 出 `code_url` 扫码 + `claim_token` 认领。**不依赖 weixinpay 插件**，收款能力本就齐备。

> 结论：不要试图把 weixinpay 插件搬到百炼/new-api；要么用我们自己的 HTTP 接口（路径 B 扫码），要么直接用 new-api 自带网页充值。

---

## 二、需要 Qcode 确认 / 协助的事项

（基于他正在用百炼 CLI 配置，请他对照我们的接口规格给结论）

- [ ] 百炼 CLI 怎么声明**自定义 HTTP 插件/工具**？能否直接 `import` 我们的 OpenAPI 文档（`bailian-openapi.yaml`）一次性生成工具？
- [ ] 一个插件能否配**两套鉴权**（下单接口固定 `X-Agent-Token`；其余用终端用户的 `Bearer sk-...`）？还是要拆成两个插件？
- [ ] 百炼自定义插件能否**渲染对话内支付卡片**？还是只能输出 `code_url` 链接/二维码文本？
- [ ] 终端用户的 API Key（`sk-...`）在百炼里如何作为**可变 Header** 传入工具（不能写死）？
- [ ] 我们的后端 `scheng.net` 是否在百炼的**出网白名单 / 可访问**？
- [ ] 是否建议**直接走机B new-api 网页充值**（已有、零开发），绕过百炼插件复杂度？

---

## 三、我们已备好的配置文件（直接给他用，不用重做）

| 文件 | 作用 |
|---|---|
| `docs/skillpay/bailian-app-config.md` | 百炼权威配置：提示词 + 7 工具 OpenAPI + 支付宝解绑红线 |
| `docs/skillpay/bailian-openapi.yaml` | 可导入百炼生成 8 个 HTTP 工具的接口规范（含两套鉴权 scheme） |
| `docs/skillpay/bailian-setup-guide.md` | 逐字段填写对照表（含"建完插件必须挂到应用"的坑） |
| `docs/skillpay/savvy-quota-topup-bailian.zip` | 上传到百炼的技能包（v2.0.1，含路径 B 降级逻辑） |

---

## 四、关键接口与凭据（给 Qcode 直接落配置）

**固定写死的平台令牌（仅「下单」接口需要）：**
```
POST /api/user/agent/wechat/topup/create
Header: X-Agent-Token: 3c0877b2ae2b5399708597d578daa051
```

**其余接口用终端用户自己的 API Key（`Authorization: Bearer sk-...`，运行时提供，勿写死）：**
```
GET  /api/user/agent/topup/status?claim_token=...
GET  /api/user/agent/ability/balance
GET  /api/user/agent/ability/usage?days=7
GET  /api/user/agent/ability/orders
POST /api/user/agent/ability/redeem        { "code": "XXX" }
POST /api/user/agent/ability/refund/apply  { "out_trade_no": "...", "reason": "..." }
GET  /api/user/agent/ability/refund/list
```

**金额规则**：0.01 ~ 5000 元，服务端裁定，技能/工具描述里不要写死数字。

**安全红线**：百炼若挂了「支付宝 AI 付」相关 MCP，先解绑（那是支付宝官方 app_id，钱不进我们账、无我们自己的 out_trade_no 可退）。

---

## 五、验收方式（任选）

- 百炼：让 agent 说"生成个微信 1 元测试订单" → 应返回 `code_url` 二维码 → 用户微信扫码付 → 订单 `success`。（会真实创建一笔待支付单，不付会超时）
- WorkBuddy（已内置 weixinpay）：调 `/api/skill/invoke` 返回 `402 + WeixinPay-Required` → 自动弹支付卡片（已实测过 0.1 元单）。
