# wechatpay-direct-v3 · 微信支付直连商户 APIv3 收款技能（公钥模式）

零依赖 Node 实现的微信支付 **直连商户 APIv3** 收款客户端，原生支持 **微信支付公钥模式**（`PUB_KEY_ID_` 开头，2024-10 后新商户号不再下发平台证书）。

> 明确不适用：服务商模式（sub_mchid / sp_mchid）。如果你需要给下级商户进件，请用微信支付合作伙伴接口。

## 能力

- 下单：Native（扫码）/ JSAPI / 小程序 / H5 / APP
- 订单：商户单号查单、微信单号查单、关单
- 退款：申请、查询
- 回调：公钥验签（自动拒绝 >5min 时间戳与 SIGNTEST 探测）→ AES-256-GCM 解密，一站式 `handleNotify`
- 调起支付签名：JSAPI/小程序（签 `package` 值）、APP（签纯 `prepayId`）
- 响应验签：API 请求响应自动验签
- 工具：`yuanToFen` / `fenToYuan`（金额一律以分为整数单位）

## 安装与配置

无第三方依赖，Node ≥ 14 直接 `require`。

```bash
export WXP_MCHID=1900000000
export WXP_APPID=wxXXXXXXXXXXXXXXXX
export WXP_CERT_SERIAL_NO=你的商户API证书序列号
export WXP_PRIVATE_KEY_PATH=./apiclient_key.pem
export WXP_APIV3_KEY=32位APIv3密钥
export WXP_PUBLIC_KEY_ID=PUB_KEY_ID_xxxxxxxx
export WXP_PUBLIC_KEY_PATH=./wxp_pub_key.pem
export WXP_NOTIFY_URL=https://your-domain.com/pay/notify
```

```javascript
const { WechatPayDirectV3 } = require('wechatpay-direct-v3');
const wxpay = new WechatPayDirectV3();      // 读环境变量
// 或 new WechatPayDirectV3({ mchId, appId, ... }) 显式传入
```

## 用法速览

```javascript
// Native 下单
const { code_url } = await wxpay.createNativeOrder({
  description: '充值宝-100元档',
  out_trade_no: 'TOPUP20260922001',
  amount: { totalYuan: 100 },
});

// JSAPI 下单 + 调起签名
const { prepay_id } = await wxpay.createJsapiOrder({
  description: '订阅卡-月卡',
  out_trade_no: 'SUB20260922001',
  amount: { total: 3000 },
  payer: { openid: 'USER_OPENID' },
});
const payParams = wxpay.buildJsapiPaySign('wxXXXX', prepay_id);

// 查单 / 关单 / 退款
await wxpay.queryOrderByOutTradeNo('TOPUP20260922001');
await wxpay.closeOrder('TOPUP20260922001');
await wxpay.createRefund({
  out_trade_no: 'TOPUP20260922001',
  out_refund_no: 'REFUND20260922001',
  amount: { refund: 10000, total: 10000 },
});

// 回调（务必拿原始 body）
const r = wxpay.handleNotify(req.headers, rawBody);
if (r.ok && r.eventType === 'TRANSACTION.SUCCESS') {
  // r.data.out_trade_no / transaction_id / amount.total — 记得幂等
}
```

## Pay Skill 改造（微信 Agent Pay X402）

v1.1.0 起内置 X402 模块（`lib/x402-pay.js`），零依赖 Node 实现 SkillHub Pay Skill 全链路：

- `X402Preorder`：第③步 X402 AI 预下单（L2 → 标准 Base64 → 5 行签名串 → SkillHub 密钥 SHA256withRSA → L1）换 `payment_code`
- `PaySkillHandler`：第④⑧⑨步统一 invoke——首次请求返回 `402 + WeixinPay-Required + X-Out-Trade-No`；支付后携 `X-Out-Trade-No` 重试 → 复用 `WechatPayDirectV3` 查单验证 → 履约返回 200，内置幂等缓存

```bash
# SkillHub 开发者密钥（商户中心 → 开发者密钥生成；仅第③步用，与 WXP_* 互不相干）
export SKILLHUB_DEVELOPER_ID=sh-XXXXXXXX
export SKILLHUB_PUB_KEY_ID=PUB_KEY_xxxx
export SKILLHUB_PRIVATE_KEY_PATH=./skillhub_dev_key.pem
export PAY_SKILL_ID=your-skill-slug
export PAY_AMOUNT_CENTS=30
```

```javascript
const { X402Preorder, PaySkillHandler } = require('wechatpay-direct-v3/lib/x402-pay');
const handler = new PaySkillHandler({
  wxpay: new WechatPayDirectV3(),
  x402: new X402Preorder(),
  fulfill: async (query) => `【付费内容】${query}`,
});
// GET/POST 统一入口：
const r = await handler.invoke({ query, headers: req.headers });
res.status(r.status).set(r.headers).json(r.body); // 402 或 200
```

协议细节见 `references/x402_protocol.md`；两套密钥（微信 API 证书 vs SkillHub 开发者密钥）与 Agent 侧 4 条指令要点见 `SKILL.md`。

## 离线自检

```bash
npm run demo        # 微信支付 V3 基础链路
npm run demo:pay    # X402 Pay Skill 链路（第③④步，全程离线 mock）
```

覆盖：请求签名、公钥验签、过期/篡改拒绝、GCM 解密往返、一站式回调、两种调起签名、金额转换；以及 X402 签名串格式、L2/Base64/L1 结构、公钥验签往返与篡改拒绝、402 返回、重试履约、幂等、关单补偿。

## 安全红线

1. 私钥 / APIv3 密钥 / SkillHub 开发者私钥只走环境变量或密钥管理，**严禁入库入仓**
2. 回调**先验签再解密再幂等处理**（微信最多重试 15 次：15s→6h）
3. 金额用"分"，避免浮点误差
4. notify_url 必须 https、外网可达、不带参数
5. H5 支付校验 Origin 白名单，下单前验证登录态

## 来源与致谢

签名/验签/回调规范依据微信支付官方《签名与验签规则》《回调通知处理》文档（微信支付 AI 接入工具箱 `wechatpay-basic-payment` 技能）。本项目为独立社区实现，与微信支付官方无隶属关系。
