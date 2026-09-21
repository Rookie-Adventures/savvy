# Pay Skill X402 协议参考

微信支付 Agent Pay X402 协议完整技术文档。本文件由SkillPay技能改造器自动生成，供开发参考。

## 协议流程（9 步时序）

```
① Agent → 商户服务    POST /skill/invoke {"query":"xxx"}
② 商户服务 → 微信支付  Native 下单（微信支付 API 证书签名）→ code_url
③ 商户服务 → 微信支付  AI 预下单（SkillHub 开发者密钥签名）→ payment_code
④ 商户服务 → Agent    HTTP 402 + WeixinPay-Required + X-Out-Trade-No
⑤ Agent → 微信支付    调用 weixinpay_pay(paymentCode=xxx)
⑥ 用户 → 微信支付     确认并完成支付
⑦ Agent → 商户服务    POST /skill/invoke + Header X-Out-Trade-No
⑧ 商户服务 → 微信支付  查单验证（微信支付 API 证书签名）→ trade_state=SUCCESS
⑨ 商户服务 → Agent    HTTP 200 + 付费内容
```

## 两套密钥（最关键的区分）

| 密钥 | 用途 | 签名算法 | 获取方式 |
|------|------|----------|----------|
| 微信支付 API 证书 | Native 下单、查单等标准支付接口 | WECHATPAY2-SHA256-RSA2048 | 微信支付商户平台申请 |
| SkillHub 开发者密钥 | AI 预下单接口 | SKILLHUB-SHA256-RSA2048 | SkillHub 商户后台一键生成 |

⚠️ AI 预下单接口**不使用**微信支付 API 证书签名，而是使用 SkillHub 颁发的开发者密钥签名。微信支付后台会向 SkillHub 验签，确认商户身份。

## 统一接口设计

一个 `POST /skill/invoke` 接口承载两种场景，通过请求头 `X-Out-Trade-No` 是否存在区分：

### 场景一：首次请求（无 X-Out-Trade-No）

创建订单 → 返回 HTTP 402。

**响应头：**
```
WeixinPay-Required: <payment_code>
X-Out-Trade-No: <out_trade_no>
```

**响应体：**
```json
{
  "code": "PAYMENT_REQUIRED",
  "message": "需要支付后才能获取内容",
  "WeixinPay": {
    "WeixinPay-Required": "payment_code_xxx",
    "prompt": "本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。"
  },
  "out_trade_no": "WX402_20260630120000abcdef123456",
  "amount": "0.01",
  "currency": "CNY"
}
```

### 场景二：支付后重试（携带 X-Out-Trade-No）

查单验证通过 → 返回 HTTP 200 + 付费内容。

**请求头：**
```
X-Out-Trade-No: <out_trade_no>
WeixinPay-Required: <payment_code>
```

**响应体：**
```json
{
  "code": "SUCCESS",
  "message": "付费内容获取成功",
  "out_trade_no": "WX402_20260630120000abcdef123456",
  "transaction_id": "4200001234202306300000000001",
  "content": "【付费内容】...",
  "already_fulfilled": false
}
```

## X402 AI 预下单

### L1 / L2 两层结构

**L2（业务 JSON）→ Base64 编码 → 放入 L1 的 `payment_required` 字段。**

#### L2 业务 JSON

```json
{
  "skill_info": {
    "skill_id": "your-skill-slug",
    "skill_version": "1.0.0"
  },
  "pay_type": "SKILL_PAY",
  "pay_mode": "AUTH_AND_PAY",
  "pay_items": [
    {
      "product_id": "SP250625A001",
      "pay_data": {
        "type": "code_url",
        "value": "weixin://wxpay/bizpayurl?pr=NwY5Mz9&groupid=00"
      }
    }
  ],
  "expires_at": "1750924500"
}
```

| 字段 | 说明 |
|------|------|
| `skill_info.skill_id` | SkillHub 发布的 Skill slug |
| `skill_info.skill_version` | Skill 版本号 |
| `pay_type` | 固定 `SKILL_PAY` |
| `pay_mode` | 固定 `AUTH_AND_PAY`（授权即支付） |
| `pay_items[].product_id` | 保留字段，格式 `SPxxx`，值任意 |
| `pay_items[].pay_data.type` | 固定 `code_url` |
| `pay_items[].pay_data.value` | 微信支付 Native 下单返回的 code_url |
| `expires_at` | 过期时间，Unix 秒级时间戳，最长 15 分钟 |

#### Base64 编码

```python
import base64, json
payment_required = base64.b64encode(
    json.dumps(l2_dict).encode("utf-8")
).decode("utf-8")
```

标准 Base64（非 URL-safe，不带换行）。

### 签名（5 行，核心难点）

签名串固定为 5 行，**每行以 `\n`（0x0A）结尾，包括最后一行**：

```
POST\n
/palmpayminiapp/clawagentpay/preorder\n
{timestamp}\n
{nonce_str}\n
{payment_required}\n
```

| 行 | 内容 |
|----|------|
| 1 | HTTP 方法，固定 `POST` |
| 2 | 请求路径，固定 `/palmpayminiapp/clawagentpay/preorder` |
| 3 | Unix 秒级时间戳（10 位数字） |
| 4 | 32 位随机字母数字串 |
| 5 | 上一步的 `payment_required` |

⚠️ 签名失败最常见的原因：最后一行漏掉换行符。

### SHA256withRSA 签名

用 SkillHub 开发者私钥对签名串做 SHA256withRSA（PKCS#1 v1.5），结果 Base64 编码：

```python
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding

private_key = serialization.load_pem_private_key(
    pem_bytes, password=None, backend=default_backend()
)
signature = private_key.sign(
    sign_string.encode("utf-8"),
    padding.PKCS1v15(),
    hashes.SHA256()
)
signature_b64 = base64.b64encode(signature).decode("utf-8")
```

### L1 请求体

```json
{
  "signature_type": "SKILLHUB-SHA256-RSA2048",
  "developer_platform": "SKILLHUB",
  "developer_id": "sh-XXXXXXXX",
  "pub_key_id": "PUB_KEY_408B07E79B8269FEC3D5D3E6AB8ED163",
  "nonce_str": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",
  "timestamp": "1750924200",
  "signature": "BASE64_RSA_SIGNATURE...",
  "payment_required": "eyJza2lsbF9pbmZvIjp7..."
}
```

| 字段 | 值 |
|------|-----|
| `signature_type` | `SKILLHUB-SHA256-RSA2048`（固定） |
| `developer_platform` | `SKILLHUB`（固定） |
| `developer_id` | 你的 SkillHub 商户号 |
| `pub_key_id` | Step 1 生成的密钥 ID |
| `nonce_str` | 32 位随机字符串，每次请求唯一 |
| `timestamp` | Unix 秒级时间戳 |
| `signature` | Base64 签名值 |
| `payment_required` | Base64 编码的 L2 JSON |

### 发送请求

```
POST https://payapp.weixin.qq.com/palmpayminiapp/clawagentpay/preorder
Content-Type: application/json
```

**成功响应：**
```json
{
  "payment_code": "xY9zAbc123def456"
}
```

## 微信支付 Native 下单

```
POST https://api.mch.weixin.qq.com/v3/pay/transactions/native
```

⚠️ 使用微信支付 V3 API 签名（WECHATPAY2-SHA256-RSA2048），需要微信支付 API 证书。建议使用官方 SDK。

**请求体：**
```json
{
  "appid": "wx1234567890abcdef",
  "mchid": "1900000001",
  "description": "Skill Pay 调用",
  "out_trade_no": "WX402_20260630120000abcdef123456",
  "notify_url": "https://example.com/pay/notify",
  "amount": {
    "total": 30,
    "currency": "CNY"
  }
}
```

**响应：**
```json
{
  "code_url": "weixin://wxpay/bizpayurl?pr=example"
}
```

## 微信支付查单

```
GET https://api.mch.weixin.qq.com/v3/pay/transactions/out-trade-no/{out_trade_no}?mchid={mchid}
```

⚠️ 同样使用微信支付 V3 API 签名。

**响应：**
```json
{
  "trade_state": "SUCCESS",
  "transaction_id": "4200001234202306300000000001"
}
```

## 商户订单号

- 长度 ≤ 32 位（微信支付要求）
- 推荐格式：`WX402_` + 时间戳(14位) + 随机串(12位) = 32 位
- 必须保证唯一性

## 幂等控制

- 同一 `out_trade_no` 只履约一次
- 重复请求返回缓存结果（`already_fulfilled: true`）
- 生产环境用数据库事务保证幂等

## WeixinPay-Required 返回方式

### 方式一：HTTP Header（推荐）

```
HTTP/1.1 402 Payment Required
WeixinPay-Required: xY9zAbc123def456
Content-Type: application/json

{
  "data": {"message": "本次查询需要付费"},
  "WeixinPay": {
    "WeixinPay-Required": "xY9zAbc123def456",
    "prompt": "本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。"
  }
}
```

建议同时在响应体中附加 `WeixinPay` 提示块，兼容只读 body 的 Agent。HTTP 状态码支持 402 或 200（兼容模式）。

### 方式二：CLI 输出

```json
{
  "WeixinPay-Required": "xY9zAbc123def456",
  "prompt": "本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。"
}
```

## 从 Demo 到生产

| 项 | Demo 状态 | 生产要求 |
|----|-----------|----------|
| 微信支付 V3 签名 | 占位实现（PLACEHOLDER） | 必须用微信支付官方 SDK 自动签名 |
| 订单存储 | 内存 map | 用数据库，事务保证幂等 |
| AI 预下单签名 | 完整实现 | 可直接复用 |
| 支付回调 | 未实现 | 实现 notify_url 回调处理 |
| 业务逻辑 | 模拟内容 | 替换为实际业务函数 |

**微信支付官方 SDK：**
- Python: `pip install wechatpayv3`
- Go: `github.com/wechatpay-apiv3/wechatpay-go`
- Java: `com.github.wechatpay-apiv3:wechatpay-java`

## Skill Prompt 编写要点

SKILL.md 中必须包含以下 4 条 Agent 指令：

1. **付费前置检查** — 检查 weixinpay 插件是否已安装
2. **请求资源** — POST 到服务接口
3. **处理 402** — 提取 WeixinPay-Required + X-Out-Trade-No，调用 weixinpay_pay
4. **支付后重试** — 携带 X-Out-Trade-No Header 重新请求（⚠️ 最容易被遗漏）

关键提醒：Body 必须与首次请求完全一致，仅通过 Header 传递订单号和支付凭证。
