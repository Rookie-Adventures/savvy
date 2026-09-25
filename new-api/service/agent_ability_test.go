package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// 额度展示单位换算：模型要把"还剩多少"讲成人话，这里错了就会把 2500000 直接念给用户
func TestQuotaToUnits(t *testing.T) {
	got := quotaToUnits(int(common.QuotaPerUnit))
	if got < 0.999 || got > 1.001 {
		t.Fatalf("1 unit expected, got %v", got)
	}
	if q := quotaToUnits(0); q != 0 {
		t.Fatalf("zero quota must be 0 unit, got %v", q)
	}
	if r := round2(1.2399); r != 1.24 {
		t.Fatalf("round2 want 1.24, got %v", r)
	}
}

// 低余额阈值：用户配了预警阈值就用他的，没配（或配 0/负数）回落到默认——续费提醒不能天天误报
func TestLowBalanceThresholdUnits(t *testing.T) {
	u := &model.User{}
	if got := lowBalanceThresholdUnits(u); got != agentLowBalanceDefaultUnits {
		t.Fatalf("default threshold want %v, got %v", agentLowBalanceDefaultUnits, got)
	}
	u.SetSetting(dto.UserSetting{QuotaWarningThreshold: 7.5})
	if got := lowBalanceThresholdUnits(u); got != 7.5 {
		t.Fatalf("configured threshold want 7.5, got %v", got)
	}
	u.SetSetting(dto.UserSetting{QuotaWarningThreshold: -1})
	if got := lowBalanceThresholdUnits(u); got != agentLowBalanceDefaultUnits {
		t.Fatalf("non-positive must fall back to default, got %v", got)
	}
}

// 续费引导链接：站点地址常带结尾斜杠，拼出 //wallet 会被当成跨域 URL
func TestAgentRechargeUrl(t *testing.T) {
	old := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = old })
	system_setting.ServerAddress = "https://scheng.net/"
	if got := agentRechargeUrl(); got != "https://scheng.net/wallet" {
		t.Fatalf("want https://scheng.net/wallet, got %q", got)
	}
	system_setting.ServerAddress = "https://scheng.net"
	if got := agentRechargeUrl(); got != "https://scheng.net/wallet" {
		t.Fatalf("want https://scheng.net/wallet, got %q", got)
	}
}

// 退款工单号：短到能口头报给用户，且不可重复（重复会让工单互相覆盖）
func TestAgentRefundTicketNo(t *testing.T) {
	a, b := model.NewAgentRefundTicketNo(), model.NewAgentRefundTicketNo()
	if len(a) != 19 { // RFD(3) + yyyymmdd(8) + 随机(8)
		t.Fatalf("ticket no too long/short: %q", a)
	}
	if a == b {
		t.Fatal("ticket no must be unique")
	}
}

func TestIsValidAgentRefundStatus(t *testing.T) {
	ok := []string{model.AgentRefundStatusPending, model.AgentRefundStatusApproved, model.AgentRefundStatusRejected}
	for _, s := range ok {
		if !model.IsValidAgentRefundStatus(s) {
			t.Fatalf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "OK", "approved ", "cancelled"} {
		if model.IsValidAgentRefundStatus(s) {
			t.Fatalf("%q should be invalid", s)
		}
	}
}
