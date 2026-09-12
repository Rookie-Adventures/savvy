package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

// 商户平台「微信支付公钥」下发单行 base64(无 PEM 头尾、可能含空格),
// 归一化后必须能被 SDK 的 LoadPublicKey 解析;已带 PEM 头尾的原样透传。
func TestNormalizeWechatPublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal pkix: %v", err)
	}
	body := base64.StdEncoding.EncodeToString(der)

	var spaced strings.Builder
	for i, r := range body {
		if i > 0 && i%64 == 0 {
			spaced.WriteByte(' ')
		}
		spaced.WriteRune(r)
	}
	pemForm := "-----BEGIN PUBLIC KEY-----\n" + body + "\n-----END PUBLIC KEY-----\n"

	for name, in := range map[string]string{
		"single-line base64": body,
		"spaced base64":      spaced.String(),
		"already PEM":        pemForm,
	} {
		got := normalizeWechatPublicKey(in)
		if name == "already PEM" && got != in {
			t.Errorf("%s: PEM 输入应原样透传", name)
		}
		if _, err := utils.LoadPublicKey(got); err != nil {
			t.Errorf("%s: LoadPublicKey failed: %v", name, err)
		}
	}
}
