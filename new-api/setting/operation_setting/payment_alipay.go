package operation_setting

// Alipay 官方直连配置。公钥模式填 AppId+AppPrivateKey+PublicKey(字符串);
// 证书模式置 AlipayIsCertMode=true 填 AppId+AppPrivateKey+AppCertSN+RootCertSN。
// 运行时 GetAlipayClient() 据此判断走哪套验签。
var (
	AlipayAppId        = ""
	AlipayAppPrivateKey = "" // PEM 应用私钥
	AlipayPublicKey    = "" // PEM 支付宝公钥(公钥模式)
	AlipayIsCertMode   = false
	AlipayAppCertSN    = "" // 应用公钥证书序列号(证书模式)
	AlipayAlipayCertSN = "" // 支付宝公钥证书序列号(证书模式)
	AlipayRootCertSN   = "" // 支付宝根证书序列号(证书模式)
	AlipayNotifyURL    = "" // 可选,空则用 GetCallbackAddress() 拼接
)

// IsAlipayConfigured reports whether admin has filled enough Alipay creds to serve.
func IsAlipayConfigured() bool {
	if AlipayAppId == "" || AlipayAppPrivateKey == "" {
		return false
	}
	if AlipayIsCertMode {
		return AlipayAppCertSN != "" && AlipayAlipayCertSN != "" && AlipayRootCertSN != ""
	}
	return AlipayPublicKey != ""
}
