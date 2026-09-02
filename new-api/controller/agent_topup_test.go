package controller

import (
	"testing"

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
