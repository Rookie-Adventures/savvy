package operation_setting

// 百炼智能体(对话下单)配置。Host 为业务空间专属端点(形如 https://ws-xxx.cn-beijing.maas.aliyuncs.com),
// Key 为该业务空间 API Key,AppId 为百炼智能体应用 ID。三者齐全才可用;缺则 handler 返回未配置提示。
var (
	AgentBailianHost  = ""
	AgentBailianKey   = ""
	AgentBailianAppId = ""
)

// IsAgentBailianConfigured reports whether admin has filled Bailian agent creds to serve.
func IsAgentBailianConfigured() bool {
	return AgentBailianHost != "" && AgentBailianKey != "" && AgentBailianAppId != ""
}
