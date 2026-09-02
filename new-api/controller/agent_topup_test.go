package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 默认额度展示类型为 USD(generalSetting.QuotaDisplayType 无公开 setter,
// TOKENS 分支不可单测赋值,由集成验收覆盖)。
func TestAgentQuotaAmountFromMoney(t *testing.T) {
	oldPrice := operation_setting.Price
	t.Cleanup(func() { operation_setting.Price = oldPrice })
	operation_setting.Price = 7.0 // 1 USD 额度 = 7 RMB

	// 对齐 getPayMoney 逆运算: money = amount * Price * groupRatio
	// 70 RMB / 7.0 / ratio(default→1) = 10 USD 额度
	if got := agentQuotaAmountFromMoney(70, "default"); got != 10 {
		t.Fatalf("want 10, got %d", got)
	}
	// 0.1 RMB → ~0.014 USD → 四舍五入 0(极小额单不入账,认领接口会拒绝)
	if got := agentQuotaAmountFromMoney(0.1, "default"); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
	// 四舍五入边界: 38.5 RMB / 7.0 = 5.5 → 6(decimal Round half away from zero)
	if got := agentQuotaAmountFromMoney(38.5, "default"); got != 6 {
		t.Fatalf("want 6, got %d", got)
	}
	// Price 误配为 0 → 兜底成 1,不 panic
	operation_setting.Price = 0
	if got := agentQuotaAmountFromMoney(70, "default"); got != 70 {
		t.Fatalf("want 70 with price guard, got %d", got)
	}
}

func TestNewClaimToken(t *testing.T) {
	a, err := newClaimToken()
	if err != nil || len(a) != 32 {
		t.Fatalf("bad token %q err %v", a, err)
	}
	b, _ := newClaimToken()
	if a == b {
		t.Fatal("tokens must be random")
	}
}

func TestGuestChatLimiter(t *testing.T) {
	oldH, oldD := operation_setting.AgentGuestChatHourLimit, operation_setting.AgentGuestChatDayLimit
	t.Cleanup(func() {
		operation_setting.AgentGuestChatHourLimit, operation_setting.AgentGuestChatDayLimit = oldH, oldD
		resetGuestChatLimiter()
	})
	resetGuestChatLimiter()
	operation_setting.AgentGuestChatHourLimit = 2
	operation_setting.AgentGuestChatDayLimit = 3

	if !allowGuestChat("1.2.3.4") {
		t.Fatal("first should pass")
	}
	if !allowGuestChat("1.2.3.4") {
		t.Fatal("second should pass")
	}
	if allowGuestChat("1.2.3.4") {
		t.Fatal("third should hit hourly limit")
	}
	if !allowGuestChat("5.6.7.8") {
		t.Fatal("other ip unaffected")
	}
	// 非法配置(<=0)回退默认值,不应把所有人锁死
	operation_setting.AgentGuestChatHourLimit = 0
	operation_setting.AgentGuestChatDayLimit = -1
	resetGuestChatLimiter()
	if !allowGuestChat("9.9.9.9") {
		t.Fatal("default limits should allow first message")
	}
}

// agentClaimDecision 是纯函数(不碰 DB),分组/零金额检查在 handler 内做。
func TestAgentClaimDecision(t *testing.T) {
	agentOrder := func(status string, userId int) *model.TopUp {
		return &model.TopUp{PaymentProvider: model.PaymentProviderAlipayAgent, Status: status, UserId: userId}
	}
	if code := agentClaimDecision(agentOrder(common.TopUpStatusSuccess, 0), 7); code != agentClaimOK {
		t.Fatalf("paid guest order should be claimable, got %d", code)
	}
	if code := agentClaimDecision(agentOrder(common.TopUpStatusSuccess, 7), 7); code != agentClaimAlreadyMine {
		t.Fatal("idempotent re-claim should report already-mine")
	}
	if code := agentClaimDecision(agentOrder(common.TopUpStatusSuccess, 8), 7); code != agentClaimTaken {
		t.Fatal("other user's claim must be rejected")
	}
	if code := agentClaimDecision(agentOrder(common.TopUpStatusPending, 0), 7); code != agentClaimNotPaid {
		t.Fatal("unpaid order must not be claimable")
	}
	notAgent := &model.TopUp{PaymentProvider: model.PaymentProviderAlipay, Status: common.TopUpStatusSuccess, UserId: 0}
	if code := agentClaimDecision(notAgent, 7); code != agentClaimNotAgentOrder {
		t.Fatal("non-agent provider must be rejected")
	}
}
