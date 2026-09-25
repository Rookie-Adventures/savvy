package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestX402SignStringIsFiveLinesEachNewlineTerminated(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const paymentRequired = "eyJhIjoxfQ=="
	ts := "1770000000"
	nonce := "abc123"

	sig, xerr := x402Sign(key, paymentRequired, ts, nonce)
	require.Nil(t, xerr)

	// 自签自验:验签口径必须与协议文档一致 —— 5 行、每行以 \n 结尾(含末行)
	signString := "POST\n" + x402SignPath + "\n" + ts + "\n" + nonce + "\n" + paymentRequired + "\n"
	assert.Equal(t, 5, countLines(signString))
	digest := sha256.Sum256([]byte(signString))
	require.NoError(t, rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig))

	// 少一个尾部 \n 必须验不过(最易写错的口径)
	badDigest := sha256.Sum256([]byte("POST\n" + x402SignPath + "\n" + ts + "\n" + nonce + "\n" + paymentRequired))
	assert.Error(t, rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, badDigest[:], sig))
}

func TestX402PaymentRequiredIsStdBase64OfL2WithoutAmount(t *testing.T) {
	restore := snapshotX402Settings(t)
	defer restore()
	operation_setting.X402SkillId = "savvy-quota-topup"
	operation_setting.X402SkillVersion = "1.0.0"

	const expiresAt = int64(1770000900)
	encoded, err := x402EncodePaymentRequired("weixin://wxpay/bizpayurl?pr=TEST", expiresAt)
	require.Nil(t, err)

	// 标准 Base64:URL-safe 字符集不该出现,且必须能被 StdEncoding 解回
	raw, derr := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, derr)
	assert.NotContains(t, encoded, "-")
	assert.NotContains(t, encoded, "_")

	var l2 map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &l2))
	assert.Equal(t, "SKILL_PAY", l2["pay_type"])
	assert.Equal(t, "AUTH_AND_PAY", l2["pay_mode"])
	assert.Equal(t, "1770000900", l2["expires_at"])
	skillInfo, ok := l2["skill_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "savvy-quota-topup", skillInfo["skill_id"])

	items, ok := l2["pay_items"].([]interface{})
	require.True(t, ok)
	require.Len(t, items, 1)
	item := items[0].(map[string]interface{})
	payData := item["pay_data"].(map[string]interface{})
	assert.Equal(t, "code_url", payData["type"])
	assert.Equal(t, "weixin://wxpay/bizpayurl?pr=TEST", payData["value"])
	// 红线:金额不在预下单报文里(实际扣款额只来自 ② 步 Native 订单)
	assert.NotContains(t, string(raw), "amount")
	assert.NotContains(t, string(raw), "total")
}

func TestX402PrivateKeyLoaderAcceptsBothFormats(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	got, lerr := loadX402PrivateKey(string(pkcs1))
	require.NoError(t, lerr, "SkillHub 下载的开发者私钥是 PKCS#1,必须能签")
	assert.Equal(t, key.N, got.N)

	pkcs8Bytes, kerr := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, kerr)
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})
	_, lerr = loadX402PrivateKey(string(pkcs8))
	require.NoError(t, lerr, "PKCS#8 也支持(自签生成的密钥常见)")

	_, lerr = loadX402PrivateKey("-----BEGIN PUBLIC KEY-----\nZm9v\n-----END PUBLIC KEY-----")
	assert.Error(t, lerr)
	_, lerr = loadX402PrivateKey("garbage")
	assert.Error(t, lerr)
}

func TestX402OutTradeNoWithinWechatLimit(t *testing.T) {
	no := X402OutTradeNo()
	assert.Len(t, no, 32)
	assert.Contains(t, no, "WX402_")
	// 前缀外仅允许大写字母与数字(微信商户单号字符集)
	for _, r := range strings.TrimPrefix(no, "WX402_") {
		assert.True(t, (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'), "illegal char in %s", no)
	}
	assert.NotEqual(t, no, X402OutTradeNo())
}

func TestX402PreorderFailClosedWithoutConfig(t *testing.T) {
	restore := snapshotX402Settings(t)
	defer restore()
	operation_setting.X402Enabled = false
	code, expires, err := X402Preorder(context.Background(), "weixin://x")
	assert.Empty(t, code)
	assert.Zero(t, expires)
	var xe *X402Error
	require.ErrorAs(t, err, &xe)
	assert.Equal(t, "CONFIG_MISSING", xe.Code)
}

func TestX402PreorderRejectsBadPrivateKey(t *testing.T) {
	restore := snapshotX402Settings(t)
	defer restore()
	operation_setting.X402Enabled = true
	operation_setting.SkillhubDeveloperId = "sh-1"
	operation_setting.SkillhubPubKeyId = "PUB_KEY_1"
	operation_setting.X402SkillId = "savvy-quota-topup"
	operation_setting.X402AmountCents = 100
	operation_setting.SkillhubPrivateKeyPEM = "not a pem"

	code, _, err := X402Preorder(context.Background(), "weixin://x")
	assert.Empty(t, code)
	var xe *X402Error
	require.ErrorAs(t, err, &xe)
	assert.Equal(t, "KEY_INVALID", xe.Code)
}

func TestX402ErrorCarriesGatewayBody(t *testing.T) {
	xe := &X402Error{Code: "HTTP_400", Message: `{"code":"INVALID_SIGNATURE"}`, Status: 400}
	assert.Contains(t, xe.Error(), "HTTP_400")
	assert.Contains(t, xe.Error(), "INVALID_SIGNATURE")
}

func countLines(s string) int {
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}

func snapshotX402Settings(t *testing.T) func() {
	t.Helper()
	enabled := operation_setting.X402Enabled
	devID := operation_setting.SkillhubDeveloperId
	pubKeyID := operation_setting.SkillhubPubKeyId
	privPEM := operation_setting.SkillhubPrivateKeyPEM
	skillID := operation_setting.X402SkillId
	version := operation_setting.X402SkillVersion
	amount := operation_setting.X402AmountCents
	return func() {
		operation_setting.X402Enabled = enabled
		operation_setting.SkillhubDeveloperId = devID
		operation_setting.SkillhubPubKeyId = pubKeyID
		operation_setting.SkillhubPrivateKeyPEM = privPEM
		operation_setting.X402SkillId = skillID
		operation_setting.X402SkillVersion = version
		operation_setting.X402AmountCents = amount
	}
}
