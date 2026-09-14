package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// WechatAppSecret / WechatMpAppId 必须注册进 OptionMap,否则后台写入重启即丢(对齐支付宝/百炼范式)。
// ponytail: OptionMap 在 InitOptionMap() 时从各 operation_setting var 重新填充;新 var 必须显式注册,
// 否则 InitOptionMap 后下面断言仍失败(红)。
func TestWechatAppSecretOptionRegistered(t *testing.T) {
	sec, mp := operation_setting.WechatAppSecret, operation_setting.WechatMpAppId
	defer func() { operation_setting.WechatAppSecret, operation_setting.WechatMpAppId = sec, mp }()
	operation_setting.WechatAppSecret = "sec-123"
	operation_setting.WechatMpAppId = "wx-mp-test"
	InitOptionMap() // 重新从各 var 填充 OptionMap(注册行生效后此处才反映)
	if common.OptionMap["WechatAppSecret"] != "sec-123" {
		t.Fatal("WechatAppSecret not registered in OptionMap")
	}
	if common.OptionMap["WechatMpAppId"] != "wx-mp-test" {
		t.Fatal("WechatMpAppId not registered in OptionMap")
	}
}
