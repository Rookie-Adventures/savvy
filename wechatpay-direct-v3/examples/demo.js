'use strict';
/**
 * 离线冒烟测试：不访问微信服务器，验证本技能的三个核心密码学环节
 *   1. 接口请求签名（5 行签名串 → Authorization 头）
 *   2. 公钥验签（模拟微信侧签名 → 本地验证）
 *   3. 回调解密（AES-256-GCM）+ JSAPI/APP 调起支付签名
 *
 * 运行：node examples/demo.js
 */

const crypto = require('crypto');
const { WechatPayDirectV3, yuanToFen, fenToYuan } = require('../lib/wechatpay-v3');

function assert(cond, label) {
  if (!cond) {
    console.error(`✗ ${label}`);
    process.exitCode = 1;
  } else {
    console.log(`✓ ${label}`);
  }
}

// ---- 准备一套测试密钥对（充当"商户私钥"与"微信支付公钥"两个角色） ----
const { privateKey, publicKey } = crypto.generateKeyPairSync('rsa', { modulusLength: 2048 });
const privateKeyPem = privateKey.export({ type: 'pkcs8', format: 'pem' });
const publicKeyPem = publicKey.export({ type: 'spki', format: 'pem' });

const wxpay = new WechatPayDirectV3({
  mchId: '1000000000',
  appId: 'wxDemoAppId000000',
  serialNo: 'TESTSERIAL000000000000000000000000',
  privateKey: privateKeyPem,
  apiV3Key: '0123456789abcdef0123456789abcdef', // 32 位
  publicKeyId: 'PUB_KEY_ID_TEST0000000000000000000000',
  publicKey: publicKeyPem,
  notifyUrl: 'https://example.com/pay/notify',
});

// ---- 1. 接口请求签名 ----
const auth = wxpay.buildAuthorization('POST', '/v3/pay/transactions/native', '{"test":1}');
assert(auth.startsWith('WECHATPAY2-SHA256-RSA2048 mchid='), '接口请求签名生成 Authorization 头');
assert(/serial_no="TESTSERIAL/.test(auth), 'Authorization 含商户证书序列号');

// 官方规范核心点：签名串第 5 行 body 为空时也以 \n 结尾
const authGet = wxpay.buildAuthorization('GET', '/v3/pay/transactions/out-trade-no/X?mchid=1', '');
assert(authGet.length > 0, 'GET 请求空 body 也能正确签名');

// ---- 2. 公钥验签（模拟微信侧对响应/回调签名） ----
const rawBody = JSON.stringify({ id: 'evt-1', resource: {} });
const ts = Math.floor(Date.now() / 1000).toString();
const nonce = crypto.randomBytes(16).toString('hex');
const message = `${ts}\n${nonce}\n${rawBody}\n`;
const wxSignature = crypto.createSign('RSA-SHA256').update(message, 'utf8').sign(privateKeyPem, 'base64');

assert(
  wxpay.verifySignature(
    {
      'Wechatpay-Timestamp': ts,
      'Wechatpay-Nonce': nonce,
      'Wechatpay-Signature': wxSignature,
      'Wechatpay-Serial': 'PUB_KEY_ID_TEST0000000000000000000000',
    },
    rawBody
  ),
  '公钥验签通过（正常签名）'
);

// 过期时间戳必须被拒绝
let rejected = false;
try {
  wxpay.verifySignature(
    {
      'Wechatpay-Timestamp': (Math.floor(Date.now() / 1000) - 3600).toString(),
      'Wechatpay-Nonce': nonce,
      'Wechatpay-Signature': wxSignature,
      'Wechatpay-Serial': 'PUB_KEY_ID_TEST0000000000000000000000',
    },
    rawBody
  );
} catch (e) {
  rejected = e.code === 'VERIFY_TIMESTAMP_EXPIRED';
}
assert(rejected, '过期时间戳（>5 分钟）被拒绝');

// 篡改 body 必须被拒绝
let tamperedRejected = false;
try {
  wxpay.verifySignature(
    {
      'Wechatpay-Timestamp': ts,
      'Wechatpay-Nonce': nonce,
      'Wechatpay-Signature': wxSignature,
      'Wechatpay-Serial': 'PUB_KEY_ID_TEST0000000000000000000000',
    },
    rawBody.replace('evt-1', 'evt-2')
  );
} catch (e) {
  tamperedRejected = e.code === 'VERIFY_FAIL';
}
assert(tamperedRejected, '报文被篡改时验签失败');

// ---- 3. 回调解密（AES-256-GCM 往返） ----
// 真实微信回调中 resource.nonce 是 ASCII 字符串，IV = 该字符串的 UTF-8 字节
const apiV3Key = '0123456789abcdef0123456789abcdef';
const plain = {
  out_trade_no: 'TOPUP20260922001',
  transaction_id: '4200001234202609220001',
  trade_state: 'SUCCESS',
  amount: { total: 10000, payer_total: 10000 },
};
const nonceStr = '0123456789ab'; // 12 字节 ASCII，与微信 nonce 用法一致
const aad = 'transaction';
const cipher = crypto.createCipheriv('aes-256-gcm', Buffer.from(apiV3Key, 'utf8'), Buffer.from(nonceStr, 'utf8'));
cipher.setAAD(Buffer.from(aad, 'utf8'));
const ct = Buffer.concat([cipher.update(JSON.stringify(plain), 'utf8'), cipher.final()]);
const ciphertext = Buffer.concat([ct, cipher.getAuthTag()]).toString('base64');
const decrypted2 = wxpay.decryptResource({ nonce: nonceStr, associated_data: aad, ciphertext });

assert(decrypted2.trade_state === 'SUCCESS' && decrypted2.out_trade_no === 'TOPUP20260922001', 'AES-256-GCM 回调解密往返一致');

// ---- 4. 回调一站式 handleNotify ----
const notifyBody = JSON.stringify({
  id: 'evt-2',
  event_type: 'TRANSACTION.SUCCESS',
  summary: '支付成功',
  resource: { nonce: nonceStr, associated_data: aad, ciphertext },
});
const ts2 = Math.floor(Date.now() / 1000).toString();
const nonce2 = crypto.randomBytes(16).toString('hex');
const msg2 = `${ts2}\n${nonce2}\n${notifyBody}\n`;
const sig2 = crypto.createSign('RSA-SHA256').update(msg2, 'utf8').sign(privateKeyPem, 'base64');
const notifyResult = wxpay.handleNotify(
  { 'Wechatpay-Timestamp': ts2, 'Wechatpay-Nonce': nonce2, 'Wechatpay-Signature': sig2, 'Wechatpay-Serial': 'PUB_KEY_ID_TEST0000000000000000000000' },
  notifyBody
);
assert(notifyResult.ok && notifyResult.eventType === 'TRANSACTION.SUCCESS', 'handleNotify 验签+解密一站式通过');
assert(WechatPayDirectV3.respondSuccess() === '{"code":"SUCCESS","message":"成功"}', '回调成功应答格式正确');

// ---- 5. 调起支付签名 ----
const jsapiSign = wxpay.buildJsapiPaySign('wxDemoAppId000000', 'wx2012345678abcdef');
assert(jsapiSign.package === 'prepay_id=wx2012345678abcdef' && jsapiSign.signType === 'RSA', 'JSAPI 调起签名：第 4 行签 package 值（带前缀）');
const appSign = wxpay.buildAppPaySign('wxDemoAppId000000', 'wx2012345678abcdef');
assert(appSign.sign && appSign.partnerId === '1000000000', 'APP 调起签名：签纯 prepayId + partnerId');

// ---- 6. 金额工具 ----
assert(yuanToFen(100) === 10000 && fenToYuan(10000) === '100.00', '元↔分转换无浮点误差');

console.log(process.exitCode ? '\n冒烟测试存在失败项' : '\n全部通过');
