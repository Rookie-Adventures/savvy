package operation_setting

import "testing"

func TestIsAlipayConfiguredPublicKeyMode(t *testing.T) {
	// 保存并复位
	origAppId, origPriv, origPub := AlipayAppId, AlipayAppPrivateKey, AlipayPublicKey
	AlipayIsCertMode = false
	defer func() {
		AlipayAppId, AlipayAppPrivateKey, AlipayPublicKey = origAppId, origPriv, origPub
		AlipayIsCertMode = false
	}()

	AlipayAppId, AlipayAppPrivateKey, AlipayPublicKey = "", "", ""
	if IsAlipayConfigured() {
		t.Fatal("empty config should not be configured")
	}
	AlipayAppId = "2021000"
	AlipayAppPrivateKey = "priv"
	AlipayPublicKey = "pub"
	if !IsAlipayConfigured() {
		t.Fatal("public-key mode with appid+priv+pub should be configured")
	}
}

func TestIsAlipayConfiguredCertMode(t *testing.T) {
	AlipayIsCertMode = true
	AlipayAppId = "2021000"
	AlipayAppCertSN = "sn1"
	AlipayRootCertSN = "sn2"
	AlipayAppPrivateKey = "priv"
	AlipayPublicKey = "" // 证书模式不读公钥
	// ponytail: brief plan-bug — test only set 2 SNs but impl (correctly per Alipay SDK)
	// requires 3: app cert SN + alipay public cert SN + root cert SN. Added the 3rd here
	// rather than weakening the impl gate.
	AlipayAlipayCertSN = "sn3"
	defer func() {
		AlipayIsCertMode = false
		AlipayAppCertSN, AlipayAlipayCertSN, AlipayRootCertSN = "", "", ""
	}()
	if !IsAlipayConfigured() {
		t.Fatal("cert mode with appid+2certSN+priv should be configured")
	}
}
