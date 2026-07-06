package operation_setting

import "testing"

func TestIsWechatConfigured(t *testing.T) {
	// 保存并复位
	origAppId, origMchID, origSerial := WechatAppId, WechatMchID, WechatMchSerial
	origAPIv3Key, origPriv, origCertPath := WechatAPIv3Key, WechatPrivateKeyPEM, WechatPlatformCertPath
	defer func() {
		WechatAppId, WechatMchID, WechatMchSerial = origAppId, origMchID, origSerial
		WechatAPIv3Key, WechatPrivateKeyPEM, WechatPlatformCertPath = origAPIv3Key, origPriv, origCertPath
	}()

	WechatAppId, WechatMchID, WechatMchSerial = "", "", ""
	WechatAPIv3Key, WechatPrivateKeyPEM, WechatPlatformCertPath = "", "", ""
	if IsWechatConfigured() {
		t.Fatal("empty config should not be configured")
	}

	WechatAppId = "wx123"
	WechatMchID = "mch"
	WechatMchSerial = "serial"
	WechatAPIv3Key = "key"
	WechatPrivateKeyPEM = "priv"
	WechatPlatformCertPath = "/path/to/platform.crt"
	if !IsWechatConfigured() {
		t.Fatal("all fields filled should be configured")
	}

	// ponytail: 用户当前真实状态 — 商户号已开但缺已认证服务号 AppId。
	// 锁定 AppId 缺失即拒绝(不卡支付宝,Task 6 GetWechatClient 返 nil → 拦下单)。
	// 待管理员填入 AppId 后此路径自然转 true。这是线上当下态,必须显式测。
	WechatAppId = ""
	if IsWechatConfigured() {
		t.Fatal("missing appid should not be configured")
	}

	// negative: MchID 缺失也必须拒,锁 gate 第二字段
	WechatAppId = "wx123"
	WechatMchID = ""
	if IsWechatConfigured() {
		t.Fatal("missing mchID should not be configured")
	}

	// negative: PlatformCertPath 留空但其他齐 → 仍 configured(刻意:验签证书可由 wechatpay-go 自动下载)
	WechatMchID = "mch"
	WechatPlatformCertPath = ""
	if !IsWechatConfigured() {
		t.Fatal("missing platform cert path should still be configured (auto-downloadable)")
	}
}
