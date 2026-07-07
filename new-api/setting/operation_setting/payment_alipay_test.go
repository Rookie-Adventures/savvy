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
	// negative: appid+priv set but public key empty must be rejected (pub-key gate at payment_alipay.go:25)
	AlipayPublicKey = ""
	if IsAlipayConfigured() {
		t.Fatal("public-key mode without PublicKey should not be configured")
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
	// ponytail: locks the exact plan-bug — dropping any one of the 3 cert SNs must fail
	// the gate at payment_alipay.go:23. The plan bug was: only 2 SNs set, 3rd (AlipayAlipayCertSN)
	// missing → IsAlipayConfigured wrongly returned true. This regression test prevents that.
	AlipayAlipayCertSN = ""
	if IsAlipayConfigured() {
		t.Fatal("cert mode missing AlipayAlipayCertSN should not be configured")
	}
}
