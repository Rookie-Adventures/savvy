---
name: wechatpay-direct-v3
slug: wechatpay-direct-v3
summary: "微信支付直连商户 APIv3 收款工具库（公钥模式，零依赖 Node）：下单/查单/关单/退款/回调验签解密 + X402 Pay Skill 改造（AI 预下单、402 触发、重试履约）。附双离线自检。"
license: MIT
description: '微信支付直连商户 APIv3 收款技能（公钥模式），零依赖 Node 实现：Native/JSAPI/小程序/H5/APP 下单、查单、关单、退款、回调验签与 AES-256-GCM 解密、调起支付签名，以及 Pay Skill 改造所需的 X402 AI 预下单（SkillHub 密钥签名）与 402 + WeixinPay-Required 一站式调用处理。配置全走环境变量，私钥不落代码。Use when user mentions "微信支付", "直连商户", "公钥模式", "PUB_KEY_ID", "Native支付", "JSAPI", "H5支付", "微信下单", "微信查单", "微信退款", "支付回调验签", "X402", "Pay Skill", "SkillPay", "Agent Pay", "WeixinPay-Required", "付费技能" or asks to "接微信支付收款"/"把服务改成付费技能".'
description_zh: "微信支付直连商户 APIv3 + 公钥模式收款实现（下单/查单/关单/退款/回调验签解密）+ X402 Pay Skill 改造（AI 预下单/402 触发/重试履约）"
description_en: "WeChat Pay direct-merchant APIv3 (public-key mode) payment skill: orders, query, close, refunds, callback verify & decrypt, plus X402 Agent Pay conversion (preorder, 402 trigger, paid retry)"
author: savvy
version: 1.1.0
displayName: "微信支付直连商户V3"
display_name: "微信支付直连商户V3"
displayNameEn: "WeChat Pay Direct Merchant V3"
display_name_en: "WeChat Pay Direct Merchant V3"
visibility: "public"
---

# 微信支付直连商户 APIv3（公钥模式）收款技能

> 适用对象：**直连商户（普通商户）**。如果你是服务商、需要 sub_mchid 子商户体系，本技能不适用。
> 2024-10 之后新开的商户号只下发**微信支付公钥**（`PUB_KEY_ID_` 开头）而不再下发平台证书——本技能原生按**公钥模式**实现验签，这正是它与其他 V2/V3 示例的关键差异。

## 能力概览

| 能力 | 方法 | 说明 |
|---|---|---|
| Native 下单 | `createNativeOrder` | 返回 `code_url`，转二维码扫码付 |
| JSAPI/小程序下单 | `createJsapiOrder` | 返回 `prepay_id`（payer.openid 必填） |
| H5 下单 | `createH5Order` | 返回 `h5_url`（scene_info 必填） |
| APP 下单 | `createAppOrder` | 返回 `prepay_id` |
| 商户单号查单 | `queryOrderByOutTradeNo` | GET，自动带 mchid |
| 微信单号查单 | `queryOrderByTransactionId` | GET |
| 关单 | `closeOrder` | 终态前关闭订单 |
| 申请退款 | `createRefund` | 支持 refund/total 或 refundYuan/totalYuan |
| 查询退款 | `queryRefund` | GET |
| 回调验签+解密 | `handleNotify` | 公钥验签 → AES-256-GCM 解密一步完成 |
| 响应/回调验签 | `verifySignature` | 自动拒绝 >5 分钟时间戳与 SIGNTEST 探测 |
| 调起支付签名 | `buildJsapiPaySign` / `buildAppPaySign` | JSAPI/小程序签 `package` 值，APP 签纯 prepayId |
| **X402 AI 预下单** | `X402Preorder.preorder` | SkillHub 开发者密钥签名换 `payment_code`（Pay Skill 第③步） |
| **Pay Skill 一站式调用** | `PaySkillHandler.invoke` | 首次 402 + `WeixinPay-Required`，重试查单履约（第④⑧⑨步） |

## 快速开始

### 1. 配置（环境变量，勿写死在代码）

```bash
WXP_MCHID=1900000000                      # 直连商户号
WXP_APPID=wxXXXXXXXXXXXXXXXX              # 按支付方式选公众号/小程序/APP 的 AppID
WXP_CERT_SERIAL_NO=XXXXXXXXXXXXXXXXXX     # 商户API证书序列号（商户平台→API安全）
WXP_PRIVATE_KEY_PATH=./apiclient_key.pem  # 商户API私钥
WXP_APIV3_KEY=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx   # 32位APIv3密钥（回调解密用）
WXP_PUBLIC_KEY_ID=PUB_KEY_ID_xxxxxxxxxxxx # 微信支付公钥ID
WXP_PUBLIC_KEY_PATH=./wxp_pub_key.pem     # 微信支付公钥
WXP_NOTIFY_URL=https://your-domain.com/pay/notify  # https、外网可达、不带参数
```

```javascript
const { WechatPayDirectV3, yuanToFen } = require('./lib/wechatpay-v3');
const wxpay = new WechatPayDirectV3(); // 全部从环境变量读取
```

### 2. Native 下单出码

```javascript
const order = await wxpay.createNativeOrder({
  description: '充值宝-100元档',
  out_trade_no: 'TOPUP20260922001',
  amount: { totalYuan: 100 },        // 也可用 total: 10000（分）
});
// order.code_url → 生成二维码给用户扫码
```

### 3. JSAPI/小程序下单 + 调起签名

```javascript
const { prepay_id } = await wxpay.createJsapiOrder({
  description: '订阅卡-月卡',
  out_trade_no: 'SUB20260922001',
  amount: { total: 3000 },           // 30 元 = 3000 分
  payer: { openid: 'user_openid' },  // 与 appid 匹配的 openid
});
const payParams = wxpay.buildJsapiPaySign('wxXXXX', prepay_id);
// → 前端 WeixinJSBridge / wx.requestPayment 直接使用
```

### 4. 回调处理（Express 示例，必须拿原始 body 验签）

```javascript
app.post('/pay/notify', express.raw({ type: '*/*' }), (req, res) => {
  const result = wxpay.handleNotify(req.headers, req.body.toString('utf8'));
  if (!result.ok) {
    res.status(401).send(wxpay.constructor.respondFail(result.error.message));
    return;
  }
  if (result.eventType === 'TRANSACTION.SUCCESS') {
    // result.data.out_trade_no / transaction_id / amount.total
    // 幂等处理：先查订单是否已入账，避免微信重试导致重复加款
  }
  res.status(200).send(wxpay.constructor.respondSuccess());
});
```

> 回调处理规范：不做登录态校验、5 秒内应答、幂等处理、先应答后异步业务。
> 微信会偶发带 `WECHATPAY/SIGNTEST/` 前缀签名的探测请求，验签失败返回 4xx 即可（本技能已自动识别）。

### 5. 退款

```javascript
await wxpay.createRefund({
  out_trade_no: 'TOPUP20260922001',
  out_refund_no: 'REFUND20260922001',
  amount: { refund: 10000, total: 10000 }, // 或 refundYuan/totalYuan
  reason: '用户申请退款',
});
```

## Pay Skill 改造（微信 Agent Pay X402）

把任意服务改造成 **SkillHub Pay Skill**（Agent 调用即自动收款的付费技能）。协议时序与字段细节见 `references/x402_protocol.md`。

### 1. 配置（两套密钥，务必区分）

```bash
# —— 微信支付 API 证书（第②步下单、第⑧步查单，WECHATPAY2-SHA256-RSA2048）——
# 复用上方 WXP_* 配置即可，无需重复配置

# —— SkillHub 开发者密钥（仅第③步 AI 预下单，SKILLHUB-SHA256-RSA2048）——
SKILLHUB_DEVELOPER_ID=sh-XXXXXXXX                        # SkillHub 商户号
SKILLHUB_PUB_KEY_ID=PUB_KEY_xxxxxxxxxxxxxxxxxxxxxxxxxxxx # 开发者公钥 ID（商户中心→开发者密钥生成）
SKILLHUB_PRIVATE_KEY_PATH=./skillhub_dev_key.pem         # 开发者私钥（仅展示一次，勿入仓库）
PAY_SKILL_ID=your-skill-slug                             # SkillHub 发布的 slug
PAY_SKILL_VERSION=1.1.0
PAY_AMOUNT_CENTS=30                                      # 单次调用价格（分）
PAY_SERVICE_NAME=付费服务
```

### 2. 一站式接入（Express 示例）

```javascript
const { WechatPayDirectV3 } = require('./lib/wechatpay-v3');
const { X402Preorder, PaySkillHandler } = require('./lib/x402-pay');

const handler = new PaySkillHandler({
  wxpay: new WechatPayDirectV3(),   // 第②步下单 + 第⑧步查单
  x402: new X402Preorder(),         // 第③步 AI 预下单（SkillHub 密钥签名）
  amountCents: 30,
  fulfill: async (query, outTradeNo, orderInfo) => {
    // ⚠️ 替换为你的付费业务逻辑；此处返回值即交付给 Agent 的 content
    return `【付费内容】${query}`;
  },
  // fulfillStore: 自己的 DB 实现（get/set），默认内存 Map，生产必须替换
});

// 第④步：首次请求 → 402 + WeixinPay-Required + X-Out-Trade-No
// 第⑦→⑨步：携 X-Out-Trade-No 重试 → 查单 SUCCESS → 200 + 付费内容
app.post('/skill/invoke', express.json(), async (req, res) => {
  try {
    const r = await handler.invoke({ query: req.body.query, headers: req.headers });
    res.status(r.status).set(r.headers).json(r.body);
  } catch (e) {
    res.status(500).json({ code: 'ERROR', message: e.message });
  }
});
```

### 3. SKILL.md 里必须写给 Agent 的 4 条指令（官方推荐原文，业务地址替换为你的服务）

```markdown
## 前置检查：当前智能体是否支持微信 AI 支付

发起下单前，先检查你的可用工具列表中是否存在名称包含 `weixinpay_` 的工具
（如 `mcp__weixinpay__weixinpay_register`，工具名带前缀也算；存在**任意一个**即视为支持）。

只以你的工具列表为证据，本文档中提到的工具名不算；无法确认时按「不支持」处理。

- 支持：静默继续本 Skill 的后续流程，不向用户提及本检查。
- 不支持：终止流程且不要发起下单。只向用户说明结论与解决办法，按智能体类型提示：
  - 合作智能体（如 WorkBuddy）：请升级智能体到最新版本后重试。
  - 开放智能体（如 OpenClaw / DeepSeek Harness）：请根据 https://skillhub.cn/install/skillhub.md
    安装 @tencent-adm/weixinpay-installer。
  - 无法判断类型：请升级智能体或安装微信支付 AI 支付插件后重试。

## 工作流程
### 第一步：请求资源
POST https://your-domain.com/skill/invoke
Body: {"query": "用户的查询内容"}

### 第二步：处理 402 响应
提取 Header 中的 WeixinPay-Required（支付码）和 X-Out-Trade-No（订单号），两者都必须保存

### 第三步：发起支付
将 WeixinPay-Required 值作为 paymentCode，调用 weixinpay_pay

### 第四步：支付成功后获取资源（⚠️ 必须执行）
POST https://your-domain.com/skill/invoke
Headers: WeixinPay-Required + X-Out-Trade-No（第二步保存的值）
Body: {"query": "原始查询内容"}（body 与第一步完全一致）
```

⚠️ **第四步是最容易被 Agent 遗漏的一步**：不携带 `X-Out-Trade-No` Header 重试，就永远拿不到付费内容。Prompt 里要用醒目措辞强调"支付成功后必须重新请求"。

### 4. X402 预下单红线（本模块已内置）

- 签名串固定 **5 行、每行以 `\n` 结尾（含最后一行）**，与微信支付 V3 签名同款红线
- L2 业务 JSON → **标准 Base64**（非 URL-safe）→ 填入 L1 `payment_required`
- `expires_at` 最长 **15 分钟**；`out_trade_no` ≤ **32 位**（`WX402_` + 14 位时间戳 + 12 位随机）
- 预下单用 **纯 Body 鉴权**，无 Authorization 头；`signature_type=SKILLHUB-SHA256-RSA2048`
- 同一订单只履约一次（幂等，重复返回 `already_fulfilled: true`）；预下单失败自动关单

## 与其他实现的关键差异（为什么这个技能存在）

| 维度 | 本技能 | 常见社区实现（如 V2 示例） |
|---|---|---|
| 模式 | **直连商户**（mchid 直接收款） | 服务商模式（sp_mchid + sub_mchid） |
| API 版本 | **APIv3**（RSA 签名） | V2（HMAC-SHA256 + XML + 双向证书） |
| 验签材料 | **微信支付公钥**（PUB_KEY_ID_） | 平台证书自动下载 |
| 依赖 | **零依赖**（node:crypto/https） | axios/xml2js/node-rsa 等 |
| 凭据管理 | 全环境变量，不落代码 | 常见硬编码 apiKey/appSecret |

## 安全红线

1. 私钥/APIv3 密钥/SkillHub 开发者私钥只放环境变量或密钥管理服务，**严禁提交仓库**
2. 回调必须**先验签再解密再处理业务**，本技能 `handleNotify` 已按此顺序
3. 幂等：微信最多重试 15 次（15s→6h），务必按 out_trade_no 去重
4. 金额一律用"分"为整数单位，避免浮点误差（工具函数 `yuanToFen`/`fenToYuan`）
5. H5 支付需校验 Origin 白名单、下单前验证登录态

## 常见签名错误速查

| 现象 | 原因 |
|---|---|
| 签名失败 | GET body 为空但末行缺 `\n`（本技能已内置） |
| 签名失败 | 换行用了 `\r\n`（本技能统一 `\n`） |
| 签名失败 | 签名与发送的 body 不一致（本技能同一字符串） |
| VERIFY_SERIAL_MISMATCH | 微信支付公钥已轮换 → 商户平台换新公钥并更新环境变量 |
| VERIFY_TIMESTAMP_EXPIRED | 本机时钟偏差 >5 分钟 → 校时 |
| HTTP_401 SIGN_ERROR | 检查 serial_no 是否为商户证书序列号 |

## 参考文档

- 官方《签名与验签规则》《回调通知处理》（微信支付 AI 接入工具箱 `wechatpay-basic-payment` 技能）
- 微信支付商户平台：https://pay.weixin.qq.com/
