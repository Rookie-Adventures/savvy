package operation_setting

// ZeroClaw 自托管智能体运行时(对话下单)配置,取代百炼托管应用。
// URL 为网关地址(形如 ws://127.0.0.1:42617),Token 为经 /pair 换取的配对凭据,
// Agent 为 [agents.<alias>] 入口名。三者齐全才可用;缺则 handler 返回未配置提示。
var (
	AgentZeroClawURL   = ""
	AgentZeroClawToken = ""
	AgentZeroClawAgent = ""
)

// 游客(未登录)聊天限额,防匿名烧 token。0 或负值视为默认值。
var (
	AgentGuestChatHourLimit = 10
	AgentGuestChatDayLimit  = 50
)

// IsAgentZeroClawConfigured reports whether admin has filled ZeroClaw gateway creds to serve.
func IsAgentZeroClawConfigured() bool {
	return AgentZeroClawURL != "" && AgentZeroClawToken != "" && AgentZeroClawAgent != ""
}
