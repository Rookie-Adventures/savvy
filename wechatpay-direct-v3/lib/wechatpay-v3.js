'use strict';
/**
 * 微信支付直连商户 APIv3 客户端（公钥模式，零依赖）
 *
 * 特性：
 *  - 直连商户（普通商户）模式，非服务商模式：无需 sub_mchid
 *  - 公钥模式验签：serial 以 PUB_KEY_ID_ 开头，用微信支付公钥验签（兼容 2024-10 后新商户号不下发平台证书的情况）
 *  - 全部配置走环境变量或构造参数，私钥/APIv3 密钥不落代码
 *  - 零第三方依赖（仅 node:crypto / node:https / node:fs）
 *
 * 签名规范（官方《签名与验签规则》）：
 *  - 接口请求签名串固定 5 行，每行以 \n 结尾（含最后一行），换行符必须是 \n 而非 \r\n
 *  - 签名时 body 是什么字符串，发送时就必须是什么字符串
 */

const crypto = require('crypto');
const https = require('https');
const fs = require('fs');

const DEFAULT_GATEWAY = 'https://api.mch.weixin.qq.com';
const MAX_CLOCK_SKEW_SECONDS = 300; // 验签时拒绝时间戳偏差超过 5 分钟的请求

class WxPayV3Error extends Error {
  /**
   * @param {string} message
   * @param {string} [code] 微信返回的 code（如 SIGN_ERROR / RESOURCE_NOT_EXISTS）
   * @param {number} [httpStatus]
   * @param {any} [detail] 原始响应体或附加信息
   */
  constructor(message, code, httpStatus, detail) {
    super(message);
    this.name = 'WxPayV3Error';
    this.code = code || 'UNKNOWN';
    this.httpStatus = httpStatus || 0;
    this.detail = detail;
  }
}

/** 支持 PEM 原文（含 \n 转义）或文件路径 */
function resolvePem(pemOrPath) {
  if (!pemOrPath) return '';
  if (pemOrPath.includes('-----BEGIN')) {
    return pemOrPath.replace(/\\n/g, '\n');
  }
  if (fs.existsSync(pemOrPath)) {
    return fs.readFileSync(pemOrPath, 'utf8');
  }
  return '';
}

/** 元转分（金额一律以"分"为整数单位） */
function yuanToFen(yuan) {
  const n = Math.round(Number(yuan) * 100);
  if (!Number.isFinite(n) || n <= 0) {
    throw new WxPayV3Error(`无效金额: ${yuan}（需为正数元）`, 'PARAM_ERROR');
  }
  return n;
}

/** 分转元（返回字符串，避免浮点误差） */
function fenToYuan(fen) {
  return (Number(fen) / 100).toFixed(2);
}

/**
 * 微信支付直连商户 APIv3 客户端（公钥模式）
 *
 * 环境变量（与构造参数同名驼峰字段二选一，构造参数优先）：
 *  - WXP_MCHID            商户号（直连商户号）
 *  - WXP_APPID            AppID（公众号/小程序/APP，按支付方式）
 *  - WXP_CERT_SERIAL_NO   商户 API 证书序列号
 *  - WXP_PRIVATE_KEY      商户 API 私钥 PEM（或 WXP_PRIVATE_KEY_PATH 指向 apiclient_key.pem）
 *  - WXP_APIV3_KEY        APIv3 密钥（32 位，用于回调解密）
 *  - WXP_PUBLIC_KEY_ID    微信支付公钥 ID（PUB_KEY_ID_ 开头）
 *  - WXP_PUBLIC_KEY       微信支付公钥 PEM（或 WXP_PUBLIC_KEY_PATH）
 *  - WXP_NOTIFY_URL       默认回调地址（https、外网可访问、不带参数）
 *  - WXP_GATEWAY          网关（默认官方生产网关，沙箱联调可覆盖）
 */
class WechatPayDirectV3 {
  constructor(options = {}) {
    const env = process.env;
    this.mchId = options.mchId || env.WXP_MCHID || '';
    this.appId = options.appId || env.WXP_APPID || '';
    this.serialNo = options.serialNo || env.WXP_CERT_SERIAL_NO || '';
    this.privateKey = resolvePem(options.privateKey || env.WXP_PRIVATE_KEY || (options.privateKeyPath || env.WXP_PRIVATE_KEY_PATH) || '');
    this.apiV3Key = options.apiV3Key || env.WXP_APIV3_KEY || '';
    this.publicKeyId = options.publicKeyId || env.WXP_PUBLIC_KEY_ID || '';
    this.publicKey = resolvePem(options.publicKey || env.WXP_PUBLIC_KEY || (options.publicKeyPath || env.WXP_PUBLIC_KEY_PATH) || '');
    this.notifyUrl = options.notifyUrl || env.WXP_NOTIFY_URL || '';
    this.gateway = String(options.gateway || env.WXP_GATEWAY || DEFAULT_GATEWAY).replace(/\/+$/, '');
    this.timeoutMs = options.timeoutMs || 10000;

    const missing = [];
    if (!this.mchId) missing.push('mchId/WXP_MCHID');
    if (!this.serialNo) missing.push('serialNo/WXP_CERT_SERIAL_NO');
    if (!this.privateKey) missing.push('privateKey/WXP_PRIVATE_KEY(_PATH)');
    if (!this.publicKey) missing.push('publicKey/WXP_PUBLIC_KEY(_PATH)');
    if (missing.length) {
      throw new WxPayV3Error(`缺少必填配置: ${missing.join(', ')}`, 'CONFIG_MISSING');
    }
    if (this.publicKeyId && !this.publicKeyId.startsWith('PUB_KEY_ID_')) {
      throw new WxPayV3Error('publicKeyId 应以 PUB_KEY_ID_ 开头（微信支付公钥模式）；若是平台证书序列号请改用证书模式', 'CONFIG_INVALID');
    }
  }

  /** 5 行签名串 → Authorization 头 */
  buildAuthorization(method, urlWithPathAndQuery, bodyString) {
    const timestamp = Math.floor(Date.now() / 1000).toString();
    const nonce = crypto.randomBytes(16).toString('hex');
    const message = `${method}\n${urlWithPathAndQuery}\n${timestamp}\n${nonce}\n${bodyString}\n`;
    const signature = crypto
      .createSign('RSA-SHA256')
      .update(message, 'utf8')
      .sign(this.privateKey, 'base64');
    return `WECHATPAY2-SHA256-RSA2048 mchid="${this.mchId}",nonce_str="${nonce}",timestamp="${timestamp}",serial_no="${this.serialNo}",signature="${signature}"`;
  }

  /**
   * 发起 V3 API 请求。
   * @param {string} method HTTP 方法
   * @param {string} path   以 / 开头的路径（含已 encode 的 query，签名与发送必须一致）
   * @param {object|null} bodyObj JSON body（GET 传 null）
   */
  request(method, path, bodyObj = null) {
    const body = bodyObj ? JSON.stringify(bodyObj) : '';
    const authorization = this.buildAuthorization(method, path, body);
    return new Promise((resolve, reject) => {
      const req = https.request(
        `${this.gateway}${path}`,
        {
          method,
          headers: {
            Authorization: authorization,
            'Content-Type': body ? 'application/json' : undefined,
            Accept: 'application/json',
            'User-Agent': 'wechatpay-direct-v3-skill/1.0.0',
          },
          timeout: this.timeoutMs,
        },
        (res) => {
          const chunks = [];
          res.on('data', (c) => chunks.push(c));
          res.on('end', () => {
            const raw = Buffer.concat(chunks).toString('utf8');
            let data = null;
            try { data = raw ? JSON.parse(raw) : null; } catch (_) { data = raw; }

            // 响应验签（公钥模式）：建议保留，官方要求校验微信签名
            if (this.publicKey && res.headers['wechatpay-signature']) {
              try {
                this.verifySignature(res.headers, raw);
              } catch (e) {
                reject(new WxPayV3Error(`响应验签失败: ${e.message}`, 'RESPONSE_VERIFY_FAIL', res.statusCode));
                return;
              }
            }

            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve(data);
            } else {
              const code = data && data.code ? data.code : 'HTTP_' + res.statusCode;
              const msg = data && (data.message || data.msg) ? (data.message || data.msg) : raw;
              reject(new WxPayV3Error(msg, code, res.statusCode, data));
            }
          });
        }
      );
      req.on('timeout', () => req.destroy(new WxPayV3Error('请求超时', 'TIMEOUT')));
      req.on('error', (e) => reject(e instanceof WxPayV3Error ? e : new WxPayV3Error(e.message, 'NETWORK_ERROR')));
      if (body) req.write(body);
      req.end();
    });
  }

  /** 组装下单 body：自动补 appid / mchid / notify_url，金额支持 total(分) 或 totalYuan */
  buildOrderBody(params) {
    const body = Object.assign({}, params);
    if (!body.appid) body.appid = this.appId;
    if (!body.mchid) body.mchid = this.mchId;
    if (!body.notify_url && this.notifyUrl) body.notify_url = this.notifyUrl;
    if (body.amount && body.amount.totalYuan !== undefined) {
      body.amount.total = yuanToFen(body.amount.totalYuan);
      delete body.amount.totalYuan;
    }
    if (!body.amount || !body.amount.total) {
      throw new WxPayV3Error('缺少 amount.total（单位分）或 amount.totalYuan（单位元）', 'PARAM_ERROR');
    }
    return body;
  }

  /** Native 下单：POST /v3/pay/transactions/native → { code_url } 二维码链接 */
  createNativeOrder(params) {
    return this.request('POST', '/v3/pay/transactions/native', this.buildOrderBody(params));
  }

  /** JSAPI/小程序下单：POST /v3/pay/transactions/jsapi → { prepay_id }（payer.openid 必填） */
  createJsapiOrder(params) {
    return this.request('POST', '/v3/pay/transactions/jsapi', this.buildOrderBody(params));
  }

  /** H5 下单：POST /v3/pay/transactions/h5 → { h5_url }（scene_info 必填） */
  createH5Order(params) {
    return this.request('POST', '/v3/pay/transactions/h5', this.buildOrderBody(params));
  }

  /** APP 下单：POST /v3/pay/transactions/app → { prepay_id } */
  createAppOrder(params) {
    return this.request('POST', '/v3/pay/transactions/app', this.buildOrderBody(params));
  }

  /** 商户订单号查单：GET /v3/pay/transactions/out-trade-no/{out_trade_no}?mchid=xxx */
  queryOrderByOutTradeNo(outTradeNo) {
    const path = `/v3/pay/transactions/out-trade-no/${encodeURIComponent(outTradeNo)}?mchid=${encodeURIComponent(this.mchId)}`;
    return this.request('GET', path, null);
  }

  /** 微信支付订单号查单：GET /v3/pay/transactions/id/{transaction_id}?mchid=xxx */
  queryOrderByTransactionId(transactionId) {
    const path = `/v3/pay/transactions/id/${encodeURIComponent(transactionId)}?mchid=${encodeURIComponent(this.mchId)}`;
    return this.request('GET', path, null);
  }

  /** 关单：POST /v3/pay/transactions/out-trade-no/{out_trade_no}/close */
  closeOrder(outTradeNo) {
    const path = `/v3/pay/transactions/out-trade-no/${encodeURIComponent(outTradeNo)}/close`;
    return this.request('POST', path, { mchid: this.mchId });
  }

  /** 申请退款：POST /v3/refund/domestic/refunds（amount.refund/total 单位分） */
  createRefund(params) {
    const body = Object.assign({}, params);
    if (!body.notify_url && this.notifyUrl) body.notify_url = this.notifyUrl;
    if (!body.amount || !body.amount.refund || !body.amount.total) {
      throw new WxPayV3Error('退款缺少 amount.refund / amount.total（单位分）', 'PARAM_ERROR');
    }
    if (!body.amount.currency) body.amount.currency = 'CNY';
    return this.request('POST', '/v3/refund/domestic/refunds', body);
  }

  /** 查询退款：GET /v3/refund/domestic/refunds/{out_refund_no} */
  queryRefund(outRefundNo) {
    return this.request('GET', `/v3/refund/domestic/refunds/${encodeURIComponent(outRefundNo)}`, null);
  }

  /**
   * 回调/响应验签（公钥模式）。
   * @param {object} headers 含 wechatpay-timestamp / wechatpay-nonce / wechatpay-signature / wechatpay-serial
   * @param {string} rawBody 原始报文（必须是未加工的原文字符串）
   */
  verifySignature(headers, rawBody) {
    const h = (k) => headers[k] || headers[k.replace(/(^|-)(\w)/g, (m) => m.toUpperCase())] || '';
    const timestamp = h('wechatpay-timestamp');
    const nonce = h('wechatpay-nonce');
    const signature = h('wechatpay-signature');
    const serial = h('wechatpay-serial');
    if (!timestamp || !nonce || !signature) {
      throw new WxPayV3Error('缺少微信支付签名头（Wechatpay-Timestamp/Nonce/Signature）', 'VERIFY_MISSING_HEADER');
    }
    // 拒绝过期请求（>5 分钟）
    const skew = Math.abs(Math.floor(Date.now() / 1000) - Number(timestamp));
    if (!Number.isFinite(skew) || skew > MAX_CLOCK_SKEW_SECONDS) {
      throw new WxPayV3Error(`签名时间戳过期（偏差 ${skew}s > ${MAX_CLOCK_SKEW_SECONDS}s）`, 'VERIFY_TIMESTAMP_EXPIRED');
    }
    // 公钥模式下 serial 应等于公钥 ID；探测请求（WECHATPAY/SIGNTEST/ 前缀）直接拒绝
    if (serial && this.publicKeyId && serial === 'WECHATPAY2-SIGNTEST-serial') {
      throw new WxPayV3Error('微信签名探测请求（SIGNTEST），按规范返回 4xx 即可', 'SIGNTEST');
    }
    if (serial && this.publicKeyId && serial !== this.publicKeyId) {
      throw new WxPayV3Error(`证书序列号不匹配: ${serial} ≠ ${this.publicKeyId}（公钥可能已轮换，请更新 WXP_PUBLIC_KEY_ID/KEY）`, 'VERIFY_SERIAL_MISMATCH');
    }
    const message = `${timestamp}\n${nonce}\n${rawBody}\n`;
    const ok = crypto.createVerify('RSA-SHA256').update(message, 'utf8').verify(this.publicKey, signature, 'base64');
    if (!ok) throw new WxPayV3Error('签名验证失败', 'VERIFY_FAIL');
    return true;
  }

  /** 回调 resource 解密（AES-256-GCM，密钥为 APIv3 密钥） */
  decryptResource(resource) {
    if (!this.apiV3Key || this.apiV3Key.length !== 32) {
      throw new WxPayV3Error('APIv3 密钥缺失或不是 32 位，无法解密回调', 'CONFIG_MISSING');
    }
    const { nonce, associated_data: aad, ciphertext } = resource || {};
    if (!nonce || !ciphertext) throw new WxPayV3Error('resource 缺少 nonce/ciphertext', 'PARAM_ERROR');
    const buf = Buffer.from(ciphertext, 'base64');
    const data = buf.subarray(0, buf.length - 16);
    const authTag = buf.subarray(buf.length - 16);
    const decipher = crypto.createDecipheriv('aes-256-gcm', Buffer.from(this.apiV3Key, 'utf8'), Buffer.from(nonce, 'utf8'));
    decipher.setAuthTag(authTag);
    if (aad) decipher.setAAD(Buffer.from(aad, 'utf8'));
    const plain = Buffer.concat([decipher.update(data), decipher.final()]).toString('utf8');
    return JSON.parse(plain);
  }

  /**
   * 一站式回调处理：先验签，再解密。
   * @returns {{ ok: boolean, eventType?: string, data?: object, error?: Error }}
   */
  handleNotify(headers, rawBody) {
    try {
      this.verifySignature(headers, rawBody);
      const wrapper = JSON.parse(rawBody);
      const data = this.decryptResource(wrapper.resource);
      return { ok: true, eventType: wrapper.event_type, summary: wrapper.summary, data };
    } catch (e) {
      return { ok: false, error: e };
    }
  }

  /** 回调成功应答（HTTP 200/204 body） */
  static respondSuccess() { return JSON.stringify({ code: 'SUCCESS', message: '成功' }); }

  /** 回调失败应答（返回 4xx/5XX + body，等待微信重试） */
  static respondFail(message) { return JSON.stringify({ code: 'FAIL', message: message || '失败' }); }

  /** JSAPI/小程序调起支付签名（4 行，第 4 行是 package 值 prepay_id=xxx） */
  buildJsapiPaySign(appId, prepayId) {
    const timeStamp = Math.floor(Date.now() / 1000).toString();
    const nonceStr = crypto.randomBytes(16).toString('hex');
    const message = `${appId}\n${timeStamp}\n${nonceStr}\nprepay_id=${prepayId}\n`;
    const paySign = crypto.createSign('RSA-SHA256').update(message, 'utf8').sign(this.privateKey, 'base64');
    return { appId, timeStamp, nonceStr, package: `prepay_id=${prepayId}`, signType: 'RSA', paySign };
  }

  /** APP 调起支付签名（4 行，第 4 行是纯 prepayId，不带前缀） */
  buildAppPaySign(appId, prepayId) {
    const timeStamp = Math.floor(Date.now() / 1000).toString();
    const nonceStr = crypto.randomBytes(16).toString('hex');
    const message = `${appId}\n${timeStamp}\n${nonceStr}\n${prepayId}\n`;
    const sign = crypto.createSign('RSA-SHA256').update(message, 'utf8').sign(this.privateKey, 'base64');
    return { appId, partnerId: this.mchId, prepayId, packageValue: 'Sign=WXPay', timeStamp, nonceStr, sign };
  }
}

module.exports = { WechatPayDirectV3, WxPayV3Error, yuanToFen, fenToYuan };
