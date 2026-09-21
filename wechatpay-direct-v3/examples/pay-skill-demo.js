'use strict';
/**
 * X402 Pay Skill 模块离线自检（不发起任何真实网络请求）
 *
 * 覆盖：第③步 X402 AI 预下单签名链路（L2 → Base64 → 5 行签名串 → SHA256withRSA → L1）
 *      第④步 统一 invoke（402 + WeixinPay-Required 返回 / X-Out-Trade-No 重试履约 / 幂等）
 * 运行：node examples/pay-skill-demo.js
 */

const crypto = require('crypto');
const assert = require('assert');
const {
  X402Preorder,
  PaySkillHandler,
  X402PayError,
  toCliOutput,
  generateOutTradeNo,
  X402_SIGN_PATH,
} = require('../lib/x402-pay');

const results = [];
function check(name, fn) {
  return Promise.resolve()
    .then(fn)
    .then(() => results.push(['✅', name]))
    .catch((e) => results.push(['❌', `${name} → ${e.message}`]));
}

// 测试用 SkillHub 开发者密钥（本地生成，非真实凭据）
const { publicKey, privateKey } = crypto.generateKeyPairSync('rsa', { modulusLength: 2048 });
const PRIVATE_KEY_PEM = privateKey.export({ type: 'pkcs8', format: 'pem' }).toString();
const PUBLIC_KEY_PEM = publicKey.export({ type: 'spki', format: 'pem' }).toString();

function makeX402(overrides = {}) {
  return new X402Preorder(
    Object.assign(
      {
        developerId: 'sh-TEST0000',
        pubKeyId: 'PUB_KEY_TESTTESTTESTTESTTESTTESTTESTTEST',
        privateKeyPem: PRIVATE_KEY_PEM,
        skillId: 'wechatpay-direct-v3',
        skillVersion: '1.1.0',
      },
      overrides
    )
  );
}

/** mock 微信支付客户端（仅覆盖 handler 用到的三个方法） */
function makeWxpayMock({ tradeState = 'SUCCESS', transactionId = '4200001234202609220000000001' } = {}) {
  const calls = { native: [], closed: [], query: [] };
  return {
    calls,
    async createNativeOrder(params) {
      calls.native.push(params);
      return { code_url: `weixin://wxpay/bizpayurl?pr=mock${calls.native.length}` };
    },
    async closeOrder(outTradeNo) { calls.closed.push(outTradeNo); },
    async queryOrderByOutTradeNo(outTradeNo) {
      calls.query.push(outTradeNo);
      return { trade_state: tradeState, transaction_id: transactionId, out_trade_no: outTradeNo };
    },
  };
}

/** mock 预下单传输层：校验 L1 并返回 payment_code；同时用公钥验签 */
function makePreorderTransport({ status = 200, text } = {}) {
  return async (url, bodyString) => {
    if (url !== `https://payapp.weixin.qq.com${X402_SIGN_PATH}`) {
      throw new Error(`预下单地址错误: ${url}`);
    }
    const body = JSON.parse(bodyString);
    transportCaptured = body;
    const signString = `POST\n${X402_SIGN_PATH}\n${body.timestamp}\n${body.nonce_str}\n${body.payment_required}\n`;
    const ok = crypto
      .createVerify('RSA-SHA256')
      .update(signString, 'utf8')
      .verify(PUBLIC_KEY_PEM, body.signature, 'base64');
    transportSignatureOk = ok;
    return { status, text: text || JSON.stringify({ payment_code: 'MOCK_PAYMENT_CODE_123' }) };
  };
}

/** stub 预下单：不发起网络请求，记录 code_url 并返回固定 payment_code（handler 测试用） */
function makeStubbedX402() {
  const x402 = makeX402();
  x402.preorderCalls = [];
  x402.preorder = async (codeUrl) => {
    x402.preorderCalls.push(codeUrl);
    return 'MOCK_PAYMENT_CODE_123';
  };
  return x402;
}

let transportCaptured = null;
let transportSignatureOk = false;

async function main() {
  // ── 第③步：X402 AI 预下单签名链路 ──
  const x402 = makeX402();

  await check('签名串为 5 行且每行以 \\n 结尾（含末行）', () => {
    const { signString, paymentRequired } = x402.buildL1Body('weixin://wxpay/bizpayurl?pr=x');
    const lines = signString.split('\n');
    assert.strictEqual(lines.length, 6); // 5 行 + 末尾空段
    assert.strictEqual(lines[5], '');
    assert.ok(signString.endsWith('\n'));
    assert.ok(!signString.includes('\r'), '不得出现 \\r');
    assert.strictEqual(lines[0], 'POST');
    assert.strictEqual(lines[1], X402_SIGN_PATH);
    assert.ok(/^\d{10}$/.test(lines[2]), '第 3 行应为 10 位 Unix 秒级时间戳');
    assert.ok(/^[A-Za-z0-9]{32}$/.test(lines[3]), '第 4 行应为 32 位随机串');
    assert.strictEqual(lines[4], paymentRequired);
  });

  await check('L2 业务 JSON 结构符合协议（skill_info/pay_type/pay_mode/pay_items/expires_at）', () => {
    const { l2Json } = x402.buildL1Body('weixin://wxpay/bizpayurl?pr=x', 1750924500);
    const l2 = JSON.parse(l2Json);
    assert.deepStrictEqual(l2.skill_info, { skill_id: 'wechatpay-direct-v3', skill_version: '1.1.0' });
    assert.strictEqual(l2.pay_type, 'SKILL_PAY');
    assert.strictEqual(l2.pay_mode, 'AUTH_AND_PAY');
    assert.strictEqual(l2.pay_items.length, 1);
    assert.strictEqual(l2.pay_items[0].pay_data.type, 'code_url');
    assert.strictEqual(l2.pay_items[0].pay_data.value, 'weixin://wxpay/bizpayurl?pr=x');
    assert.ok(/^SP[0-9A-F]{8}$/.test(l2.pay_items[0].product_id), `product_id 格式: ${l2.pay_items[0].product_id}`);
    assert.strictEqual(l2.expires_at, '1750924500');
  });

  await check('payment_required 为标准 Base64（非 URL-safe），可解码还原 L2', () => {
    const { paymentRequired, l2Json } = x402.buildL1Body('weixin://wxpay/bizpayurl?pr=x');
    assert.ok(!paymentRequired.includes('-') && !paymentRequired.includes('_'));
    assert.strictEqual(Buffer.from(paymentRequired, 'base64').toString('utf8'), l2Json);
  });

  await check('L1 请求体字段齐全（signature_type/platform/developer_id/pub_key_id）', () => {
    const { body } = x402.buildL1Body('weixin://wxpay/bizpayurl?pr=x');
    assert.strictEqual(body.signature_type, 'SKILLHUB-SHA256-RSA2048');
    assert.strictEqual(body.developer_platform, 'SKILLHUB');
    assert.strictEqual(body.developer_id, 'sh-TEST0000');
    assert.strictEqual(body.pub_key_id, 'PUB_KEY_TESTTESTTESTTESTTESTTESTTESTTEST');
    assert.ok(body.signature.length > 100);
  });

  await check('SHA256withRSA 签名可用对应公钥验签通过', () => {
    const { body, signString } = x402.buildL1Body('weixin://wxpay/bizpayurl?pr=x');
    const reconstructed = `POST\n${X402_SIGN_PATH}\n${body.timestamp}\n${body.nonce_str}\n${body.payment_required}\n`;
    assert.strictEqual(reconstructed, signString);
    const ok = crypto.createVerify('RSA-SHA256').update(signString, 'utf8').verify(PUBLIC_KEY_PEM, body.signature, 'base64');
    assert.ok(ok, '公钥验签应通过');
  });

  await check('payment_required 被篡改后验签失败', () => {
    const { body } = x402.buildL1Body('weixin://wxpay/bizpayurl?pr=x');
    const tampered = body.payment_required.slice(0, -4) + 'AAAA';
    const signString = `POST\n${X402_SIGN_PATH}\n${body.timestamp}\n${body.nonce_str}\n${tampered}\n`;
    const ok = crypto.createVerify('RSA-SHA256').update(signString, 'utf8').verify(PUBLIC_KEY_PEM, body.signature, 'base64');
    assert.ok(!ok, '篡改后验签应失败');
  });

  await check('preorder（mock 传输层）返回 payment_code 且 L1 验签通过', async () => {
    transportCaptured = null;
    transportSignatureOk = false;
    const code = await x402.preorder('weixin://wxpay/bizpayurl?pr=x', makePreorderTransport());
    assert.strictEqual(code, 'MOCK_PAYMENT_CODE_123');
    assert.ok(transportSignatureOk, 'mock 端用公钥验签应通过');
    assert.ok(transportCaptured && transportCaptured.payment_required);
  });

  await check('preorder 非 2xx 抛 X402PayError 并携带状态码', async () => {
    await assert.rejects(
      () => x402.preorder('weixin://wxpay/bizpayurl?pr=x', makePreorderTransport({ status: 401, text: '{"code":"SIGN_ERROR"}' })),
      (e) => e instanceof X402PayError && e.httpStatus === 401 && e.code === 'SIGN_ERROR'
    );
  });

  await check('preorder 响应缺 payment_code 报 MISSING_PAYMENT_CODE', async () => {
    await assert.rejects(
      () => x402.preorder('weixin://wxpay/bizpayurl?pr=x', makePreorderTransport({ status: 200, text: '{}' })),
      (e) => e instanceof X402PayError && e.code === 'MISSING_PAYMENT_CODE'
    );
  });

  await check('商户订单号：WX402_ 前缀、总长 32 位', () => {
    const no = generateOutTradeNo();
    assert.ok(no.startsWith('WX402_'));
    assert.strictEqual(no.length, 32, `实际长度 ${no.length}`);
  });

  // ── 第④步：统一 invoke（402 返回 / 重试履约 / 幂等） ──
  await check('首次请求 → HTTP 402 + WeixinPay-Required + X-Out-Trade-No 响应头与 WeixinPay 提示块', async () => {
    const wxpay = makeWxpayMock();
    const x402stub = makeStubbedX402();
    const handler = new PaySkillHandler({
      wxpay,
      x402: x402stub,
      amountCents: 30,
      serviceName: '天气查询',
      fulfill: () => '内容',
    });
    const res = await handler.invoke({ query: '北京天气' });
    assert.strictEqual(res.status, 402);
    assert.strictEqual(res.headers['WeixinPay-Required'], 'MOCK_PAYMENT_CODE_123');
    assert.ok(res.headers['X-Out-Trade-No'].startsWith('WX402_'));
    assert.strictEqual(res.body.code, 'PAYMENT_REQUIRED');
    assert.strictEqual(res.body.WeixinPay['WeixinPay-Required'], 'MOCK_PAYMENT_CODE_123');
    assert.ok(res.body.WeixinPay.prompt.includes('weixinpay_pay'));
    assert.strictEqual(res.body.amount, '0.30');
    assert.strictEqual(res.body.currency, 'CNY');
    // 下单参数检查：金额 30 分 + 32 位订单号；预下单收到的正是 Native 返回的 code_url
    assert.strictEqual(wxpay.calls.native[0].amount.total, 30);
    assert.strictEqual(wxpay.calls.native[0].out_trade_no, res.headers['X-Out-Trade-No']);
    assert.deepStrictEqual(x402stub.preorderCalls, ['weixin://wxpay/bizpayurl?pr=mock1']);
  });

  await check('预下单失败时自动关单并抛 X402PayError', async () => {
    const wxpay = makeWxpayMock();
    const failingX402 = makeX402();
    failingX402.preorder = async () => {
      throw new X402PayError('mock 预下单失败', 'PREORDER_FAIL');
    };
    const handler = new PaySkillHandler({ wxpay, x402: failingX402, fulfill: () => '内容' });
    await assert.rejects(
      () => handler.invoke({ query: 'x' }),
      (e) => e instanceof X402PayError && e.code === 'PREORDER_FAIL'
    );
    assert.strictEqual(wxpay.calls.closed.length, 1, '应调用 closeOrder 清理脏订单');
  });

  await check('支付成功后携 X-Out-Trade-No 重试 → 200 + 付费内容（transaction_id 透传）', async () => {
    const wxpay = makeWxpayMock();
    const handler = new PaySkillHandler({ wxpay, x402: makeStubbedX402(), fulfill: (q, no) => `【付费内容】${q}@${no}` });
    const first = await handler.invoke({ query: '北京天气' });
    const outTradeNo = first.headers['X-Out-Trade-No'];
    const res = await handler.invoke({ query: '北京天气', headers: { 'X-Out-Trade-No': outTradeNo, 'weixinpay-required': 'MOCK' } });
    assert.strictEqual(res.status, 200);
    assert.strictEqual(res.body.code, 'SUCCESS');
    assert.strictEqual(res.body.out_trade_no, outTradeNo);
    assert.strictEqual(res.body.transaction_id, '4200001234202609220000000001');
    assert.strictEqual(res.body.already_fulfilled, false);
    assert.ok(res.body.content.includes('北京天气'));
    assert.deepStrictEqual(wxpay.calls.query, [outTradeNo]);
  });

  await check('幂等：同单重复重试返回缓存 already_fulfilled=true 且不再查单', async () => {
    const wxpay = makeWxpayMock();
    const handler = new PaySkillHandler({ wxpay, x402: makeStubbedX402(), fulfill: (q) => `内容:${q}` });
    const first = await handler.invoke({ query: '上海天气' });
    const no = first.headers['X-Out-Trade-No'];
    await handler.invoke({ headers: { 'X-Out-Trade-No': no } });
    const res = await handler.invoke({ headers: { 'X-Out-Trade-No': no } });
    assert.strictEqual(res.status, 200);
    assert.strictEqual(res.body.already_fulfilled, true);
    assert.strictEqual(wxpay.calls.query.length, 1, '第二次不应再查单');
  });

  await check('未支付重试（NOTPAY）→ 402 PAYMENT_NOT_COMPLETED', async () => {
    const wxpay = makeWxpayMock({ tradeState: 'NOTPAY' });
    const handler = new PaySkillHandler({ wxpay, x402: makeStubbedX402(), fulfill: () => '内容' });
    const first = await handler.invoke({ query: 'x' });
    const res = await handler.invoke({ headers: { 'X-Out-Trade-No': first.headers['X-Out-Trade-No'] } });
    assert.strictEqual(res.status, 402);
    assert.strictEqual(res.body.code, 'PAYMENT_NOT_COMPLETED');
    assert.strictEqual(res.body.trade_state, 'NOTPAY');
  });

  await check('toCliOutput 输出 WeixinPay-Required + prompt（CLI 场景）', () => {
    const out = toCliOutput('CODE123');
    assert.strictEqual(out['WeixinPay-Required'], 'CODE123');
    assert.ok(out.prompt.includes('paymentCode'));
  });

  await check('缺配置时报 CONFIG_MISSING（SKILLHUB_* 全缺）', () => {
    const saved = { ...process.env };
    ['SKILLHUB_DEVELOPER_ID', 'SKILLHUB_PUB_KEY_ID', 'SKILLHUB_PRIVATE_KEY', 'SKILLHUB_PRIVATE_KEY_PATH', 'PAY_SKILL_ID'].forEach(
      (k) => delete process.env[k]
    );
    try {
      assert.throws(() => new X402Preorder(), (e) => e instanceof X402PayError && e.code === 'CONFIG_MISSING');
    } finally {
      Object.assign(process.env, saved);
    }
  });

  // ── 汇总 ──
  const failed = results.filter(([s]) => s === '❌');
  for (const [s, name] of results) console.log(`${s} ${name}`);
  console.log(`\n${results.length - failed.length}/${results.length} 项通过`);
  if (failed.length) {
    process.exitCode = 1;
  } else {
    console.log('🎉 X402 Pay Skill 模块自检全部通过（第③④步链路完整）');
  }
}

main().catch((e) => {
  console.error('自检执行异常:', e);
  process.exitCode = 1;
});
