package operation_setting

import (
	"os"
	"strconv"
	"strings"
)

// SkillPay（微信 Agent Pay X402）付费技能配置。
// 全环境变量注入（SKILLPAY_*），私钥不落代码/仓库；由 main.go 在 common.InitEnv() 后调用
// InitSkillPayFromEnv() 装载（.env 由 godotenv 在 main 里先加载，package init 时机太早读不到）。

var (
	// 总开关：关闭时 /api/skill/invoke 返回 503
	SkillPayEnabled = false
	// 单次调用价格（分），默认 10 分 = 0.1 元
	SkillPayPriceFen = 10
	// SkillHub 发布的 slug 与版本（L2 skill_info）
	SkillPaySkillId      = ""
	SkillPaySkillVersion = "1.0.0"
	// SkillHub 开发者密钥（仅用于 X402 AI 预下单签名，SKILLHUB-SHA256-RSA2048；
	// 与微信支付 API 证书 WXP/* 两套密钥严格分离，勿混用）
	SkillPayDeveloperId   = "" // 形如 sh-XXXXXXXX
	SkillPayPubKeyId      = "" // 形如 PUB_KEY_xxxx
	SkillPayPrivateKeyPEM = "" // RSA2048 私钥 PEM 原文（支持 \n 转义）或文件路径
	// 履约专用系统 token 与模型名：付款后服务端用它走 /v1/chat/completions 完成一次 AI 问答
	SkillPayRelayToken = ""
	SkillPayRelayModel = ""
)

func IsSkillPayConfigured() bool {
	return SkillPayEnabled && SkillPayPriceFen > 0 && SkillPaySkillId != "" &&
		SkillPayDeveloperId != "" && SkillPayPubKeyId != "" && SkillPayPrivateKeyPEM != "" &&
		SkillPayRelayToken != "" && SkillPayRelayModel != ""
}

// InitSkillPayFromEnv 从环境变量装载 SkillPay 配置（main.go 在 common.InitEnv() 后调用）。
func InitSkillPayFromEnv() {
	SkillPayEnabled = envBool("SKILLPAY_ENABLED", false)
	SkillPayPriceFen = envInt("SKILLPAY_PRICE_FEN", 10)
	SkillPaySkillId = envStr("SKILLPAY_SKILL_ID", "")
	SkillPaySkillVersion = envStr("SKILLPAY_SKILL_VERSION", "1.0.0")
	SkillPayDeveloperId = envStr("SKILLPAY_DEVELOPER_ID", "")
	SkillPayPubKeyId = envStr("SKILLPAY_PUB_KEY_ID", "")
	SkillPayPrivateKeyPEM = envPEM("SKILLPAY_PRIVATE_KEY", "SKILLPAY_PRIVATE_KEY_PATH")
	SkillPayRelayToken = envStr("SKILLPAY_RELAY_TOKEN", "")
	SkillPayRelayModel = envStr("SKILLPAY_RELAY_MODEL", "")
}

// —— 本包内的小工具（不引 common，避免依赖方向复杂化）——

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// envPEM 支持 PEM 原文（含 \n 转义）或私钥文件路径两种注入方式。
func envPEM(directKey, pathKey string) string {
	if v := os.Getenv(directKey); v != "" {
		return strings.ReplaceAll(v, "\\n", "\n")
	}
	if p := os.Getenv(pathKey); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			return string(b)
		}
	}
	return ""
}
