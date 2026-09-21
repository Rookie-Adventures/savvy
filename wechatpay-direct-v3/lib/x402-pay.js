'use strict';
/**
 * 微信支付 Agent Pay X402 模块（SkillPay / Pay Skill，零依赖）
 *
 * 职责（对应 Pay Skill 9 步时序）：
 *  - 第③步：X402 AI 预下单 —— SkillHub 开发者密钥签名（SKILLHUB-SHA256-RSA2048），
 *    调 https://payapp.weixin.qq.com/palmpayminiapp/clawagentpay/preorder 换 payment_code
 *  - 第④步：统一 invoke 接口 —— 首次请求返回 HTTP 402 + WeixinPay-Required + X-Out-Trade-No；
 *    支付后携 X-Out-Trade-No 重试 → 查单验证（第⑧步，复用 wechatpay-v3 客户端）→ 200 交付付费内容（第⑨步）
 *  - 第②步 Native 下单与第⑧步查单直接复用同目录 wechatpay-v3.js（微信支付 API 证书签名）
 *
 * ⚠️ 两套密钥务必区分：
 *  - 微信支付 API 证书（WXP_*）：Native 下单、查单 —— WECHATPAY2-SHA256-RSA2048
 *  - SkillHub 开发者密钥（SKILLHUB_*）：仅 AI 预下单 —— SKILLHUB-SHA256-RSA2048
 *
 * 签名规范：签名串固定 5 行，每行以 \n 结尾（包括最后一行），与 wechatpay-v3 同款红线。
 */

const crypto = require('crypto');
const https = require('https');
const fs = require('fs');

const X402_HOST = 'payapp.weixin.qq.com';
const X402_SIGN_PATH = '/palmpayminiapp/clawagentpay/preorder';
const X402_PREORDER_URL = `https://${X402_HOST}${X402_SIGN_PATH}`;

const SIGNATURE_TYPE = 'SKILLHUB-SHA256-RSA2048';
const DEVELOPER_PLATFORM = 'SKILLHUB';
const PAY_TYPE = 'SKILL_PAY';
const PAY_MODE = 'AUTH_AND_PAY';
const ORDER_PREFIX = 'WX402_';
const DEFAULT_EXPIRES_SECONDS = 900; // payment_code 有效期上限 15 分钟

class X402PayError extends Error {
  /**
   * @param {string} message
   * @param {string} [code]
   * @param {number} [httpStatus]
   * @param {any} [detail]
   */
  constructor(message, code, httpStatus, detail) {
    super(message);
    this.name = 'X402PayError';
    this.code = code || 'UNKNOWN';
    this.httpStatus = httpStatus || 0;
    this.detail = detail;
  }
}

/** 支持 PEM 原文（含 \n 转义）或文件路径 */
function resolvePem(pemOrPath) {
  if (!pemOrPath) return '';
  if (pemOrPath.includes('-----BEGIN')) return pemOrPath.replace(/\\n/g, '\n');
  if (fs.existsSync(pemOrPath)) return fs.readFileSync(pemOrPath, 'utf8');
  return '';
}

/** 商户订单号：WX402_ + 时间戳(14位) + 随机串(12位) = 32 位（微信支付上限 32） */
function generateOutTradeNo() {
  const ts = new Date()
    .toISOString()
    .replace(/[-:TZ.]/g, '')
    .slice(0, 14);
  const rand = crypto.randomBytes(6).toString('hex'); // 12 位小写 hex
  return `${ORDER_PREFIX}${ts}${rand}`;
}

/** 32 位随机字母数字串（预下单 nonce_str） */
function generateNonceStr(length = 32) {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let s = '';
  const bytes = crypto.randomBytes(length);
  for (let i = 0; i < length; i++) s += chars[bytes[i] % chars.length];
  return s;
}

/** 保留字段 product_id：SP + 8 位大写十六进制 */
function generateProductId() {
  return 'SP' + crypto.randomBytes(4).toString('hex').toUpperCase();
}

/**
 * X402 AI 预下单客户端（第③步）
 *
 * 环境变量（与构造参数同名驼峰字段二选一，构造参数优先）：
 *  - SKILLHUB_DEVELOPER_ID       SkillHub 商户号（sh-XXXXXXXX）
 *  - SKILLHUB_PUB_KEY_ID         开发者公钥 ID（PUB_KEY_ 开头）
 *  - SKILLHUB_PRIVATE_KEY        开发者私钥 PEM（或 SKILLHUB_PRIVATE_KEY_PATH 指向 PEM 文件）
 *  - PAY_SKILL_ID / PAY_SKILL_VERSION  SkillHub 发布的 slug 与版本
 *  - X402_GATEWAY                预下单网关（默认官方生产地址，测试可覆盖）
 */
class X402Preorder {
  constructor(options = {}) {
    const env = process.env;
    this.developerId = options.developerId || env.SKILLHUB_DEVELOPER_ID || '';
    this.pubKeyId = options.pubKeyId || env.SKILLHUB_PUB_KEY_ID || '';
    this.privateKeyPem = resolvePem(
      options.privateKeyPem || env.SKILLHUB_PRIVATE_KEY || (options.privateKeyPath || env.SKILLHUB_PRIVATE_KEY_PATH) || ''
    );
    this.skillId = options.skillId || env.PAY_SKILL_ID || '';
    this.skillVersion = options.skillVersion || env.PAY_SKILL_VERSION || '1.0.0';
    this.gateway = String(options.gateway || env.X402_GATEWAY || X402_PREORDER_URL);
    this.timeoutMs = options.timeoutMs || 10000;

    const missing = [];
    if (!this.developerId) missing.push('developerId/SKILLHUB_DEVELOPER_ID');
    if (!this.pubKeyId) missing.push('pubKeyId/SKILLHUB_PUB_KEY_ID');
    if (!this.privateKeyPem) missing.push('privateKeyPem/SKILLHUB_PRIVATE_KEY(_PATH)');
    if (!this.skillId) missing.push('skillId/PAY_SKILL_ID');
    if (missing.length) {
      throw new X402PayError(`缺少必填配置: ${missing.join(', ')}`, 'CONFIG_MISSING');
    }
  }

  /** L2 业务 JSON（skill_info + pay_items(code_url) + expires_at） */
  buildL2Json(codeUrl, expiresAt) {
    if (!codeUrl) throw new X402PayError('buildL2Json 缺少 codeUrl', 'PARAM_ERROR');
    const exp = String(expiresAt || Math.floor(Date.now() / 1000) + DEFAULT_EXPIRES_SECONDS);
    return JSON.stringify({
      skill_info: { skill_id: this.skillId, skill_version: this.skillVersion },
      pay_type: PAY_TYPE,
      pay_mode: PAY_MODE,
      pay_items: [{ product_id: generateProductId(), pay_data: { type: 'code_url', value: codeUrl } }],
      expires_at: exp,
    });
  }

  /** 5 行签名串：POST\n{path}\n{timestamp}\n{nonce}\n{payment_required}\n（末行也有 \n） */
  buildSignString(timestamp, nonceStr, paymentRequired) {
    return `POST\n${X402_SIGN_PATH}\n${timestamp}\n${nonceStr}\n${paymentRequired}\n`;
  }

  /** SHA256withRSA（PKCS#1 v1.5）→ Base64 */
  sign(signString) {
    return crypto.createSign('RSA-SHA256').update(signString, 'utf8').sign(this.privateKeyPem, 'base64');
  }

  /** 组装 L1 请求体（返回 body 与签名串，便于离线自检） */
  buildL1Body(codeUrl, expiresAt) {
    const l2Json = this.buildL2Json(codeUrl, expiresAt);
    const paymentRequired = Buffer.from(l2Json, 'utf8').toString('base64'); // 标准 Base64，非 URL-safe
    const timestamp = Math.floor(Date.now() / 1000).toString();
    const nonceStr = generateNonceStr(32);
    const signString = this.buildSignString(timestamp, nonceStr, paymentRequired);
    const body = {
      signature_type: SIGNATURE_TYPE,
      developer_platform: DEVELOPER_PLATFORM,
      developer_id: this.developerId,
      pub_key_id: this.pubKeyId,
      nonce_str: nonceStr,
      timestamp,
      signature: this.sign(signString),
      payment_required: paymentRequired,
    };
    return { body, signString, l2Json, paymentRequired };
  }

  /**
   * 调用预下单接口换 payment_code。
   * @param {string} codeUrl 微信 Native 下单返回的 code_url
   * @param {function} [transport] 自定义传输层 (url, bodyString, headers, timeoutMs) => Promise<{status, text}>，测试注入用
   * @returns {Promise<string>} payment_code
   */
  async preorder(codeUrl, transport) {
    const { body } = this.buildL1Body(codeUrl);
    const bodyString = JSON.stringify(body);
    const doRequest = transport || defaultTransport;
    const { status, text } = await doRequest(
      this.gateway,
      bodyString,
      { 'Content-Type': 'application/json', Accept: 'application/json' },
      this.timeoutMs
    );
    let data = null;
    try { data = text ? JSON.parse(text) : null; } catch (_) { /* 保留原始文本 */ }
    if (status < 200 || status >= 300) {
      const code = data && data.code ? data.code : 'HTTP_' + status;
      throw new X402PayError(`X402 预下单失败: ${text}`, code, status, data);
    }
    const paymentCode = data && data.payment_code;
    if (!paymentCode) {
      throw new X402PayError(`X402 预下单响应缺少 payment_code: ${text}`, 'MISSING_PAYMENT_CODE', status, data);
    }
    return paymentCode;
  }
}

/** 默认 HTTPS 传输层（纯 Body 鉴权，不用 Authorization 头） */
function defaultTransport(url, bodyString, headers, timeoutMs) {
  return new Promise((resolve, reject) => {
    const req = https.request(
      url,
      { method: 'POST', headers, timeout: timeoutMs },
      (res) => {
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => resolve({ status: res.statusCode, text: Buffer.concat(chunks).toString('utf8') }));
      }
    );
    req.on('timeout', () => req.destroy(new X402PayError('预下单请求超时', 'TIMEOUT')));
    req.on('error', (e) => reject(e instanceof X402PayError ? e : new X402PayError(e.message, 'NETWORK_ERROR')));
    req.write(bodyString);
    req.end();
  });
}

/**
 * Pay Skill 统一调用处理器（第④⑧⑨步）
 *
 * 通过请求头 X-Out-Trade-No 是否存在区分两种场景：
 *  - 首次请求（无）：Native 下单 → 预下单 → 返回 402 + WeixinPay-Required + X-Out-Trade-No
 *  - 支付后重试（有）：查单验证 → SUCCESS 则履约返回 200；未支付返回 402 PAYMENT_NOT_COMPLETED
 *
 * 幂等：同一 out_trade_no 只履约一次，重复请求返回缓存（already_fulfilled: true）。
 * ⚠️ 默认幂等存储为内存 Map，生产环境请通过 fulfillStore 注入数据库实现。
 */
class PaySkillHandler {
  /**
   * @param {object} options
   * @param {import('./wechatpay-v3').WechatPayDirectV3} options.wxpay   微信支付客户端（下单/查单）
   * @param {X402Preorder} options.x402                                  X402 预下单客户端
   * @param {number} [options.amountCents]                               默认订单金额（分）
   * @param {string} [options.serviceName]                               服务名（提示话术）
   * @param {function} [options.fulfill]                                 履约函数 (query, outTradeNo, orderInfo) => content
   * @param {object} [options.fulfillStore]                              幂等存储，需实现 get/set(outTradeNo, content)
   */
  constructor(options = {}) {
    const env = process.env;
    this.wxpay = options.wxpay;
    this.x402 = options.x402;
    this.amountCents = options.amountCents || Number(env.PAY_AMOUNT_CENTS) || 1;
    this.serviceName = options.serviceName || env.PAY_SERVICE_NAME || '付费服务';
    this.fulfill = options.fulfill || ((query, outTradeNo) => `【付费内容】查询: ${query} | 订单: ${outTradeNo}`);
    this._store = options.fulfillStore || {
      _map: new Map(),
      get(k) { return this._map.get(k); },
      set(k, v) { this._map.set(k, v); },
    };
  }

  /**
   * 统一入口。
   * @param {object} params
   * @param {string} params.query        用户查询内容（重试时必须与首次完全一致）
   * @param {object} [params.headers]    原始请求头（大小写不敏感地取 X-Out-Trade-No）
   * @param {string} [params.outTradeNo] 也可直接传订单号
   * @returns {Promise<{status: number, headers: object, body: object}>}
   */
  async invoke(params = {}) {
    const headers = params.headers || {};
    const headerKey = Object.keys(headers).find((k) => k.toLowerCase() === 'x-out-trade-no');
    const outTradeNo = params.outTradeNo || (headerKey ? headers[headerKey] : '');
    if (outTradeNo) return this.handleRetry(params.query, outTradeNo);
    return this.handleFirstRequest(params.query);
  }

  /** 场景一：首次请求 → Native 下单 → 预下单 → 402 */
  async handleFirstRequest(query, orderParams = {}) {
    const outTradeNo = generateOutTradeNo();
    const amount = orderParams.amount || { total: this.amountCents, currency: 'CNY' };

    let codeUrl;
    try {
      const order = await this.wxpay.createNativeOrder(
        Object.assign(
          {
            description: orderParams.description || `${this.serviceName} Pay Skill 调用`,
            out_trade_no: outTradeNo,
            amount,
          },
          orderParams.extra || {}
        )
      );
      codeUrl = order.code_url;
    } catch (e) {
      throw new X402PayError(`微信支付下单失败: ${e.message}`, 'NATIVE_ORDER_FAIL', e.httpStatus, e.detail);
    }

    let paymentCode;
    try {
      paymentCode = await this.x402.preorder(codeUrl);
    } catch (e) {
      // 预下单失败应关单，避免留下未支付脏订单
      try { await this.wxpay.closeOrder(outTradeNo); } catch (_) { /* 关单失败不掩盖原错误 */ }
      throw e instanceof X402PayError ? e : new X402PayError(`X402 预下单失败: ${e.message}`, 'PREORDER_FAIL');
    }

    const amountYuan = (Number(amount.total) / 100).toFixed(2);
    return {
      status: 402,
      headers: {
        'WeixinPay-Required': paymentCode,
        'X-Out-Trade-No': outTradeNo,
        'Content-Type': 'application/json',
      },
      body: {
        code: 'PAYMENT_REQUIRED',
        message: `本次${this.serviceName}需要支付 ¥${amountYuan}`,
        WeixinPay: {
          'WeixinPay-Required': paymentCode,
          prompt:
            '本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。',
        },
        out_trade_no: outTradeNo,
        amount: amountYuan,
        currency: 'CNY',
      },
    };
  }

  /** 场景二：支付后重试 → 查单验证 → 履约 200 */
  async handleRetry(query, outTradeNo) {
    const cached = this._store.get(outTradeNo);
    if (cached !== undefined) {
      return {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
        body: {
          code: 'SUCCESS',
          message: '付费内容获取成功',
          out_trade_no: outTradeNo,
          content: cached,
          already_fulfilled: true,
        },
      };
    }

    let orderInfo;
    try {
      orderInfo = await this.wxpay.queryOrderByOutTradeNo(outTradeNo);
    } catch (e) {
      throw new X402PayError(`查单失败: ${e.message}`, 'QUERY_ORDER_FAIL', e.httpStatus, e.detail);
    }

    if (orderInfo.trade_state !== 'SUCCESS') {
      return {
        status: 402,
        headers: { 'Content-Type': 'application/json' },
        body: {
          code: 'PAYMENT_NOT_COMPLETED',
          message: `支付未完成，当前状态: ${orderInfo.trade_state || 'UNKNOWN'}`,
          out_trade_no: outTradeNo,
          trade_state: orderInfo.trade_state,
        },
      };
    }

    const content = await this.fulfill(query, outTradeNo, orderInfo);
    this._store.set(outTradeNo, content);
    return {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
      body: {
        code: 'SUCCESS',
        message: '付费内容获取成功',
        out_trade_no: outTradeNo,
        transaction_id: orderInfo.transaction_id || '',
        content,
        already_fulfilled: false,
      },
    };
  }
}

/** CLI 输出格式（本地 CLI 服务场景，无 HTTP 头可用时） */
function toCliOutput(paymentCode) {
  return {
    'WeixinPay-Required': paymentCode,
    prompt: '本次使用微信支付，请将 WeixinPay-Required 的值作为 paymentCode 交给 weixinpay_pay，以向用户申请支付授权。',
  };
}

module.exports = {
  X402Preorder,
  PaySkillHandler,
  X402PayError,
  toCliOutput,
  generateOutTradeNo,
  generateNonceStr,
  X402_SIGN_PATH,
  X402_PREORDER_URL,
  SIGNATURE_TYPE,
  PAY_TYPE,
  PAY_MODE,
};
