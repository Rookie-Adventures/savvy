package service

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
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 微信支付 Agent Pay X402：AI 预下单（第③步）。
// 纯 Body 鉴权（无 Authorization 头），SkillHub 开发者密钥签名 —— 与微信支付 API 证书
// （WECHATPAY2-*，走 controller.GetWechatClient）完全分离，两套密钥严禁混用。
const (
	SkillPayX402URL       = "https://payapp.weixin.qq.com/palmpayminiapp/clawagentpay/preorder"
	SkillPayX402SignPath  = "/palmpayminiapp/clawagentpay/preorder"
	skillPaySignatureType = "SKILLHUB-SHA256-RSA2048"
	skillPayPlatform      = "SKILLHUB"
	skillPayPayType       = "SKILL_PAY"
	skillPayPayMode       = "AUTH_AND_PAY"
)

// BuildSkillPaySignString 构造 5 行签名串：每行以 \n 结尾（含最后一行）。
// 这是签名失败最常见根因（末行漏 \n / 混入 \r\n），独立成导出函数便于离线测试。
func BuildSkillPaySignString(timestamp, nonceStr, paymentRequired string) string {
	return fmt.Sprintf("POST\n%s\n%s\n%s\n%s\n", SkillPayX402SignPath, timestamp, nonceStr, paymentRequired)
}

// BuildSkillPayL2Json 构造 L2 业务 JSON（skill_info + pay_items + expires_at）。
// payType 按下单方式选：code_url(Native) / prepay_id(JSAPI/小程序/APP) / h5_url(H5)，
// orderValue 为对应下单接口的原样返回值。expires_at 最长 15 分钟；product_id 为保留字段。
func BuildSkillPayL2Json(skillId, skillVersion, payType, orderValue string) (string, error) {
	if orderValue == "" {
		return "", errors.New("order value is empty")
	}
	if payType != "code_url" && payType != "prepay_id" && payType != "h5_url" {
		return "", fmt.Errorf("invalid pay_data.type: %s", payType)
	}
	l2 := map[string]interface{}{
		"skill_info": map[string]string{
			"skill_id":      skillId,
			"skill_version": skillVersion,
		},
		"pay_type": skillPayPayType,
		"pay_mode": skillPayPayMode,
		"pay_items": []map[string]interface{}{{
			"product_id": "SP" + common.GetRandomString(8),
			"pay_data": map[string]string{
				"type":  payType,
				"value": orderValue,
			},
		}},
		"expires_at": fmt.Sprintf("%d", time.Now().Unix()+900),
	}
	b, err := common.Marshal(l2)
	if err != nil {
		return "", fmt.Errorf("marshal L2: %w", err)
	}
	return string(b), nil
}

// SignSHA256WithRSABase64 用 SkillHub 开发者私钥（PKCS#8/PKCS#1 PEM）对签名串做 SHA256withRSA（PKCS#1 v1.5）。
func SignSHA256WithRSABase64(privateKeyPEM, signString string) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", errors.New("invalid private key pem")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// 兼容 PKCS#1（BEGIN RSA PRIVATE KEY）
		if rsaKey, err1 := x509.ParsePKCS1PrivateKey(block.Bytes); err1 == nil {
			return rsaSignBase64(rsaKey, signString)
		}
		return "", fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("private key is not RSA")
	}
	return rsaSignBase64(rsaKey, signString)
}

func rsaSignBase64(key *rsa.PrivateKey, signString string) (string, error) {
	digest := sha256.Sum256([]byte(signString))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("rsa sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// SkillPayX402Preorder 第③步：下单标识 → X402 AI 预下单 → payment_code。
// payType：code_url(Native) / prepay_id(JSAPI/小程序/APP) / h5_url(H5)；orderValue 为下单原样返回值。
// 流程：L2 → 标准 Base64（非 URL-safe）→ 5 行签名串 → SHA256withRSA → L1 → POST。
func SkillPayX402Preorder(payType, orderValue string) (string, error) {
	l2Json, err := BuildSkillPayL2Json(operation_setting.SkillPaySkillId, operation_setting.SkillPaySkillVersion, payType, orderValue)
	if err != nil {
		return "", err
	}
	paymentRequired := base64.StdEncoding.EncodeToString([]byte(l2Json))

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonceStr := common.GetRandomString(32)
	signString := BuildSkillPaySignString(timestamp, nonceStr, paymentRequired)
	signature, err := SignSHA256WithRSABase64(operation_setting.SkillPayPrivateKeyPEM, signString)
	if err != nil {
		return "", fmt.Errorf("sign preorder: %w", err)
	}

	l1, err := common.Marshal(map[string]string{
		"signature_type":     skillPaySignatureType,
		"developer_platform": skillPayPlatform,
		"developer_id":       operation_setting.SkillPayDeveloperId,
		"pub_key_id":         operation_setting.SkillPayPubKeyId,
		"nonce_str":          nonceStr,
		"timestamp":          timestamp,
		"signature":          signature,
		"payment_required":   paymentRequired,
	})
	if err != nil {
		return "", fmt.Errorf("marshal L1: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(SkillPayX402URL, "application/json", bytes.NewReader(l1))
	if err != nil {
		return "", fmt.Errorf("post preorder: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.LogError(context.Background(), fmt.Sprintf("skillpay x402 preorder failed: http=%d body=%s", resp.StatusCode, skillPayTruncate(string(raw), 300)))
		return "", fmt.Errorf("preorder http %d", resp.StatusCode)
	}
	var out struct {
		PaymentCode string `json:"payment_code"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.PaymentCode == "" {
		return "", fmt.Errorf("preorder response missing payment_code: %s", skillPayTruncate(string(raw), 300))
	}
	return out.PaymentCode, nil
}

func skillPayTruncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
