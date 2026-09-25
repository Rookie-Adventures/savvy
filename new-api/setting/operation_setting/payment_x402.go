package operation_setting

// 微信 AI 支付(X402 / Pay Skill)直连配置。
// 两套密钥刻意分离:
//   - 微信支付 API 证书(Wechat* in payment_wechat.go) → 第②步 Native 下单、查单
//   - SkillHub 开发者密钥(本文件)                     → 第③步 AI 预下单(签名算法不同,不可混用)
var (
	X402Enabled           = false // 总开关:关闭时 invoke 直接 503,不产生任何订单
	SkillhubDeveloperId   = ""    // SkillHub 商户号 sh-XXXXXXXX
	SkillhubPubKeyId      = ""    // 开发者公钥 ID PUB_KEY_xxxx(商户中心生成,只展示一次的是私钥)
	SkillhubPrivateKeyPEM = ""    // 开发者私钥 PEM(SKILLHUB-SHA256-RSA2048 签名用)
	X402SkillId           = ""    // SkillHub 发布的 slug,如 savvy-quota-topup
	X402SkillVersion      = "1.0.0"
	// X402AmountCents 单次调用价格(分)。真实扣款额在 ② 步 Native 下单时定,
	// 此处为唯一 SKU 口径。配额按 Money(元,精确到分)折算;TopUp.Amount 是
	// 整数「元」台账列,非整元会向下取整,故建议配成 100 的整数倍。
	X402AmountCents = 100
	X402ServiceName = "Savvy 配额充值"
	// X402MpOAuthURL 服务号网页授权地址模板(免登录认领)。{redirect_uri} 会被
	// urlencode 后的认领页地址替换,例:
	//   https://open.weixin.qq.com/connect/oauth2/authorize?appid=WXAPPID&redirect_uri={redirect_uri}&response_type=code&scope=snsapi_base&state=x402#wechat_redirect
	// 留空则认领页降级为「会话内认领 / 先登录再认领」。
	X402MpOAuthURL = ""
)

// IsX402KeysReady — 预下单最小可用集(不含总开关)。开关校验与运行时判定共用,
// 避免「开开关时 IsX402Configured 因开关尚未生效而恒为 false」的死锁。
func IsX402KeysReady() bool {
	return SkillhubDeveloperId != "" && SkillhubPubKeyId != "" &&
		SkillhubPrivateKeyPEM != "" && X402SkillId != "" && X402AmountCents > 0
}

// IsX402Configured — 总开关 + 密钥齐备。缺任一即 fail-closed。
func IsX402Configured() bool {
	return X402Enabled && IsX402KeysReady()
}
