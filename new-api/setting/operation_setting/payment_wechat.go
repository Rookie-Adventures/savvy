package operation_setting

// Wechat Pay 官方直连(APIv3)配置。需已认证服务号 AppId + 商户号 + 证书三件 + APIv3 密钥。
// 用户已开商户号缺 AppId(待已认证服务号拿到)。代码不阻塞支付宝,IsConfigured=false 即拦下单。
var (
	WechatAppId            = ""
	WechatMchID            = "" // 商户号
	WechatMchSerial        = "" // 商户证书序列号
	WechatAPIv3Key         = "" // APIv3 密钥(32 位)
	WechatPrivateKeyPEM    = "" // 商户 API 私钥 PEM
	WechatPlatformCertPath = "" // 微信平台证书路径(验签)
	// 微信支付公钥(新模式):2024-10 起新商户号不再下发平台证书,只发公钥。
	// 两者齐则走公钥验签,否则退回平台证书自动下载(老商户号)。
	WechatPayPublicKeyId = "" // 形如 PUB_KEY_ID_xxxxxxxx
	WechatPayPublicKey   = "" // 微信支付公钥内容(PEM)
)

// IsWechatConfigured reports whether admin has filled enough WeChat creds.
// ponytail: 刻意不校验 WechatPlatformCertPath — wechatpay-go 可自动下载平台证书,
// 路径非必填。AppId 在用户当前态缺失 → 返 false → Task 6 GetWechatClient 返 nil。
func IsWechatConfigured() bool {
	return WechatAppId != "" && WechatMchID != "" && WechatMchSerial != "" &&
		WechatAPIv3Key != "" && WechatPrivateKeyPEM != ""
}
