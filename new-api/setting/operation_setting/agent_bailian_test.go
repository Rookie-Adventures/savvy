package operation_setting

import "testing"

func TestIsAgentBailianConfigured(t *testing.T) {
	origHost, origKey, origApp := AgentBailianHost, AgentBailianKey, AgentBailianAppId
	defer func() {
		AgentBailianHost, AgentBailianKey, AgentBailianAppId = origHost, origKey, origApp
	}()

	AgentBailianHost, AgentBailianKey, AgentBailianAppId = "", "", ""
	if IsAgentBailianConfigured() {
		t.Fatal("empty config should not be configured")
	}

	AgentBailianHost = "https://ws-x.cn-beijing.maas.aliyuncs.com"
	AgentBailianKey = "sk-xxx"
	AgentBailianAppId = "app1"
	if !IsAgentBailianConfigured() {
		t.Fatal("host+key+appid should be configured")
	}

	AgentBailianAppId = ""
	if IsAgentBailianConfigured() {
		t.Fatal("missing appid should not be configured")
	}
}
