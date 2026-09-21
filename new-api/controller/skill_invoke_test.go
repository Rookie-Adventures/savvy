package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// —— X402 签名链路（service 层纯函数，离线可测）——

func TestSkillPaySignStringFormat(t *testing.T) {
	s := service.BuildSkillPaySignString("1750924200", "abcdefghabcdefghabcdefghabcdefgh", "eyJ0ZXN0IjoxfQ==")
	lines := strings.Split(s, "\n")
	if len(lines) != 6 || lines[5] != "" { // 5 行 + 末尾空段
		t.Fatalf("签名串应为 5 行且末行含 \\n，实际: %q", s)
	}
	if lines[0] != "POST" {
		t.Fatalf("第 1 行应为 POST: %q", lines[0])
	}
	if lines[1] != "/palmpayminiapp/clawagentpay/preorder" {
		t.Fatalf("第 2 行应为预下单路径: %q", lines[1])
	}
	if lines[2] != "1750924200" || lines[3] != "abcdefghabcdefghabcdefghabcdefgh" || lines[4] != "eyJ0ZXN0IjoxfQ==" {
		t.Fatalf("签名串内容错误: %q", s)
	}
	if strings.Contains(s, "\r") {
		t.Fatalf("签名串不得包含 \\r")
	}
}

func TestSkillPayL2JsonStructure(t *testing.T) {
	l2, err := service.BuildSkillPayL2Json("savvy-ai-qa", "1.0.0", "code_url", "weixin://wxpay/bizpayurl?pr=x")
	if err != nil {
		t.Fatalf("build L2: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(l2), &m); err != nil {
		t.Fatalf("L2 非法 JSON: %v", err)
	}
	skillInfo, _ := m["skill_info"].(map[string]interface{})
	if skillInfo == nil || skillInfo["skill_id"] != "savvy-ai-qa" || skillInfo["skill_version"] != "1.0.0" {
		t.Fatalf("skill_info 错误: %v", m["skill_info"])
	}
	if m["pay_type"] != "SKILL_PAY" || m["pay_mode"] != "AUTH_AND_PAY" {
		t.Fatalf("pay_type/pay_mode 错误: %v", m)
	}
	items, _ := m["pay_items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("pay_items 应为 1 项")
	}
	item := items[0].(map[string]interface{})
	payData := item["pay_data"].(map[string]interface{})
	if payData["type"] != "code_url" || payData["value"] != "weixin://wxpay/bizpayurl?pr=x" {
		t.Fatalf("pay_data 错误: %v", payData)
	}
	if exp, ok := m["expires_at"].(string); !ok || len(exp) != 10 {
		t.Fatalf("expires_at 应为 10 位 Unix 秒: %v", m["expires_at"])
	}
}

func TestSkillPayL2Base64RoundTrip(t *testing.T) {
	l2, _ := service.BuildSkillPayL2Json("savvy-ai-qa", "1.0.0", "code_url", "weixin://x")
	pr := base64.StdEncoding.EncodeToString([]byte(l2))
	if strings.ContainsAny(pr, "-_") {
		t.Fatalf("必须使用标准 Base64（非 URL-safe）")
	}
	decoded, err := base64.StdEncoding.DecodeString(pr)
	if err != nil || string(decoded) != l2 {
		t.Fatalf("Base64 往返失败: %v", err)
	}
}

func TestSkillPayL2PayDataTypes(t *testing.T) {
	// FAQ：pay_data.type 按下单方式选 code_url/prepay_id/h5_url
	for _, pt := range []string{"code_url", "prepay_id", "h5_url"} {
		l2, err := service.BuildSkillPayL2Json("savvy-ai-qa", "1.0.0", pt, "ORDER_VALUE")
		if err != nil {
			t.Fatalf("pay_type %s 应合法: %v", pt, err)
		}
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(l2), &m)
		item := m["pay_items"].([]interface{})[0].(map[string]interface{})
		pd := item["pay_data"].(map[string]interface{})
		if pd["type"] != pt || pd["value"] != "ORDER_VALUE" {
			t.Fatalf("pay_data 错误: %v", pd)
		}
	}
	if _, err := service.BuildSkillPayL2Json("savvy-ai-qa", "1.0.0", "bogus_type", "V"); err == nil {
		t.Fatalf("非法 pay_type 应报错")
	}
}

func TestSkillPaySignAndVerify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	privDER, _ := x509.MarshalPKCS8PrivateKey(key)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	pr := base64.StdEncoding.EncodeToString([]byte(`{"t":1}`))
	signString := service.BuildSkillPaySignString("1750924200", "nonce32nonce32nonce32nonce32xx", pr)

	sig, err := service.SignSHA256WithRSABase64(privPEM, signString)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}

	// 公钥验签通过
	digest := sha256.Sum256([]byte(signString))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], mustB64(t, sig)); err != nil {
		t.Fatalf("公钥验签应通过: %v", err)
	}

	// 篡改 payment_required 后验签必须失败
	tampered := service.BuildSkillPaySignString("1750924200", "nonce32nonce32nonce32nonce32xx", pr[:len(pr)-2]+"AA")
	tDigest := sha256.Sum256([]byte(tampered))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, tDigest[:], mustB64(t, sig)); err == nil {
		t.Fatalf("篡改后验签应失败")
	}
	_ = pubPEM
}

// —— Handler 门禁（SKILLPAY 未启用 → 503）——

func TestSkillInvokeDisabledReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := operation_setting.SkillPayEnabled
	defer func() { operation_setting.SkillPayEnabled = saved }()
	operation_setting.SkillPayEnabled = false

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/skill/invoke", strings.NewReader(`{"query":"hi"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	SkillInvoke(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用时应返回 503，实际 %d: %s", w.Code, w.Body.String())
	}
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("bad base64 signature: %v", err)
	}
	return b
}
