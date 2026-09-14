package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// JSAPI 单例在配置缺失时必须返 nil(对齐 Native 的 nil-guard 范式),
// 否则 handler 会越过友好拒绝去调 SDK 导致 panic。
func TestGetWechatJsapiClientNilWhenUnconfigured(t *testing.T) {
	operation_setting.WechatAppId = ""
	operation_setting.WechatMchID = ""
	operation_setting.WechatMchSerial = ""
	operation_setting.WechatAPIv3Key = ""
	operation_setting.WechatPrivateKeyPEM = ""
	// ponytail: 复位 JSAPI 单例,防其他测试初始化后本测静默越过 nil-guard(fork-safe)。
	t.Cleanup(func() { wechatJsapiSvc = nil })

	if GetWechatJsapiClient() != nil {
		t.Fatal("GetWechatJsapiClient should return nil when not configured")
	}
}

// 铁律:公钥模式决策点必须随配置切换(2026-09-12 事故根因=两端模式不对称)。
// 直接断言 wechatUsePublicKeyVerifier() 的分支选择,不依赖真实公钥可解析。
func TestWechatUsePublicKeyVerifierBranch(t *testing.T) {
	// 公钥模式:两项都设 → true
	operation_setting.WechatPayPublicKeyId = "PUB_KEY_ID_test"
	operation_setting.WechatPayPublicKey = "any"
	if !wechatUsePublicKeyVerifier() {
		t.Fatal("public-key mode should be true when both id and key set")
	}
	// 退化:仅设其一 → false(走平台证书下载器分支)
	operation_setting.WechatPayPublicKey = ""
	if wechatUsePublicKeyVerifier() {
		t.Fatal("public-key mode should be false when key empty")
	}
	operation_setting.WechatPayPublicKeyId = ""
	operation_setting.WechatPayPublicKey = "any"
	if wechatUsePublicKeyVerifier() {
		t.Fatal("public-key mode should be false when id empty")
	}
}
