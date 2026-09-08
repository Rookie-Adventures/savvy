package operation_setting

// 企业微信「微信客服」配置:官网与小程序拉起在线客服会话用。
// KfUrl 形如 https://work.weixin.qq.com/kfid/kfxxxx,在企微后台-微信客服-客服账号详情里取。
var (
	WeComCorpId = "" // 企业 ID
	WeComKfUrl  = "" // 微信客服链接
)

// IsWeComKfConfigured 客服入口是否可用;经 /api/status 下发给前端与小程序。
func IsWeComKfConfigured() bool {
	return WeComCorpId != "" && WeComKfUrl != ""
}
