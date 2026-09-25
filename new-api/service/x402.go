package service

// 微信 AI 支付(X402 / Pay Skill)第③步:AI 预下单客户端。
// 签名体系 = SkillHub 开发者密钥(SKILLHUB-SHA256-RSA2048),与微信支付 API 证书
// (第②步下单/查单)完全独立,不可混用。纯 Body 鉴权,无 Authorization 头。
//
// 红线(官方协议 x402_protocol.md):
//   - 签名串固定 5 行、每行以 \n 结尾(含最后一行)
//   - L2 → 标准 Base64(非 URL-safe)→ L1.payment_required
//   - expires_at 最长 900s;out_trade_no ≤32 位
//   - pay_data.type: code_url(Native)/prepay_id(JSAPI)/h5_url(H5)

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	x402PreorderURL   = "https://payapp.weixin.qq.com/palmpayminiapp/clawagentpay/preorder"
	x402SignPath      = "/palmpayminiapp/clawagentpay/preorder"
	x402SignatureType = "SKILLHUB-SHA256-RSA2048"
	x402Platform      = "SKILLHUB"
	x402PayType       = "SKILL_PAY"
	x402PayMode       = "AUTH_AND_PAY"
	x402MaxTTLSeconds = 900
)

type X402Error struct {
	Code    string // CONFIG_MISSING / HTTP_<n> / MISSING_PAYMENT_CODE / ...
	Message string
	Status  int
}

func (e *X402Error) Error() string { return e.Code + ": " + e.Message }

type x402PayData struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type x402PayItem struct {
	ProductId string      `json:"product_id"`
	PayData   x402PayData `json:"pay_data"`
}

type x402SkillInfo struct {
	SkillId      string `json:"skill_id"`
	SkillVersion string `json:"skill_version"`
}

// x402L2 — 业务 JSON(Base64 后填入 L1.payment_required)。刻意不含金额:
// 扣款额由 ②步 Native 下单的 code_url 决定,X402 报文只携带技能与订单指认。
type x402L2 struct {
	SkillInfo x402SkillInfo `json:"skill_info"`
	PayType   string        `json:"pay_type"`
	PayMode   string        `json:"pay_mode"`
	PayItems  []x402PayItem `json:"pay_items"`
	ExpiresAt string        `json:"expires_at"`
}

// loadX402PrivateKey — SkillHub 开发者私钥加载。
//
// 必须自己兜 PKCS#1:wechatpay-go v0.2.21 的 utils.LoadPrivateKey 只认
// "PRIVATE KEY"(PKCS#8),而 SkillHub 商户中心下载的开发者私钥是
// "RSA PRIVATE KEY"(PKCS#1)。直接用 SDK loader 会稳定报
// "the kind of PEM should be PRVATE KEY",表现为「密钥明明对了却签不了」。
func loadX402PrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("PEM 解码失败(请粘贴含 -----BEGIN ... 的完整内容)")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 内不是 RSA 私钥")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("不支持的私钥类型 %q(需 RSA PRIVATE KEY 或 PRIVATE KEY)", block.Type)
	}
}

// X402Preorder 用 SkillHub 开发者私钥把 Native 下单返回的 code_url 换成 payment_code。
// 返回的 expiresAt 即 hold 行过期时间(与 payment_code 同寿命,15 分钟硬上限)。
func X402Preorder(ctx context.Context, codeUrl string) (paymentCode string, expiresAt int64, err error) {
	if !operation_setting.IsX402Configured() {
		return "", 0, &X402Error{Code: "CONFIG_MISSING", Message: "X402 未启用或 SkillHub 开发者密钥未配齐"}
	}
	if strings.TrimSpace(codeUrl) == "" {
		return "", 0, &X402Error{Code: "PARAM_ERROR", Message: "code_url 为空(②步未下单成功)"}
	}
	privKey, kerr := loadX402PrivateKey(operation_setting.SkillhubPrivateKeyPEM)
	if kerr != nil {
		return "", 0, &X402Error{Code: "KEY_INVALID", Message: "开发者私钥解析失败: " + kerr.Error()}
	}

	expiresAt = time.Now().Unix() + x402MaxTTLSeconds
	paymentRequired, jerr := x402EncodePaymentRequired(codeUrl, expiresAt)
	if jerr != nil {
		return "", 0, jerr
	}

	nonce := x402Random(32)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig, serr := x402Sign(privKey, paymentRequired, ts, nonce)
	if serr != nil {
		return "", 0, serr
	}

	l1 := map[string]string{
		"signature_type":     x402SignatureType,
		"developer_platform": x402Platform,
		"developer_id":       operation_setting.SkillhubDeveloperId,
		"pub_key_id":         operation_setting.SkillhubPubKeyId,
		"nonce_str":          nonce,
		"timestamp":          ts,
		"signature":          base64.StdEncoding.EncodeToString(sig),
		"payment_required":   paymentRequired,
	}
	body, _ := json.Marshal(l1)

	req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, x402PreorderURL, bytes.NewReader(body))
	if rerr != nil {
		return "", 0, &X402Error{Code: "REQUEST_BUILD_FAILED", Message: rerr.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	hres, herr := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if herr != nil {
		return "", 0, &X402Error{Code: "NETWORK_ERROR", Message: herr.Error()}
	}
	defer hres.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(hres.Body, 1<<16))
	if hres.StatusCode < 200 || hres.StatusCode >= 300 {
		return "", 0, &X402Error{Code: fmt.Sprintf("HTTP_%d", hres.StatusCode), Message: strings.TrimSpace(string(raw)), Status: hres.StatusCode}
	}
	var out struct {
		PaymentCode string `json:"payment_code"`
	}
	if json.Unmarshal(raw, &out) != nil || out.PaymentCode == "" {
		return "", 0, &X402Error{Code: "MISSING_PAYMENT_CODE", Message: strings.TrimSpace(string(raw)), Status: hres.StatusCode}
	}
	return out.PaymentCode, expiresAt, nil
}

// x402EncodePaymentRequired — L2 业务 JSON → 标准 Base64(非 URL-safe)。
// 刻意不含金额:扣款额由 ② 步 Native 下单的 code_url 决定,X402 报文只携带
// 技能与订单指认。
func x402EncodePaymentRequired(codeUrl string, expiresAt int64) (string, error) {
	l2 := x402L2{
		SkillInfo: x402SkillInfo{
			SkillId:      operation_setting.X402SkillId,
			SkillVersion: operation_setting.X402SkillVersion,
		},
		PayType:   x402PayType,
		PayMode:   x402PayMode,
		PayItems:  []x402PayItem{{ProductId: "SP" + x402Random(8), PayData: x402PayData{Type: "code_url", Value: codeUrl}}},
		ExpiresAt: fmt.Sprintf("%d", expiresAt),
	}
	l2Json, err := json.Marshal(l2)
	if err != nil {
		return "", &X402Error{Code: "L2_MARSHAL_FAILED", Message: err.Error()}
	}
	return base64.StdEncoding.EncodeToString(l2Json), nil
}

// x402Sign — 5 行签名串,每行以 \n 结尾(含最后一行);SHA256withRSA(PKCS#1 v1.5)。
func x402Sign(privKey *rsa.PrivateKey, paymentRequired, ts, nonce string) ([]byte, error) {
	signString := "POST\n" + x402SignPath + "\n" + ts + "\n" + nonce + "\n" + paymentRequired + "\n"
	digest := sha256.Sum256([]byte(signString))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, digest[:])
	if err != nil {
		return nil, &X402Error{Code: "SIGN_FAILED", Message: err.Error()}
	}
	return sig, nil
}

// X402OutTradeNo — WX402_ + 14 位时间戳 + 12 位随机 = 32 位(微信商户单号硬上限)。
func X402OutTradeNo() string {
	return fmt.Sprintf("WX402_%s%s", time.Now().Format("20060102150405"), x402Random(12))
}

const x402Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

func x402Random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("x402: crypto/rand unavailable: " + err.Error())
	}
	for i := range b {
		b[i] = x402Alphabet[int(b[i])%len(x402Alphabet)]
	}
	return string(b)
}
