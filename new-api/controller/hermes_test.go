package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToVOFreeRunningSetsRemainingMinutes(t *testing.T) {
	// 甲/丙 验: FREE + RUNNING + expires_at 2h 后 → RemainingMinutes 必非 nil
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	inst := &service.HermesInstance{
		InstanceID: "i1",
		Status:     "RUNNING",
		Plan:       "FREE",
		ExpiresAt:  future,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	vo := toVO(inst)
	require.NotNil(t, vo.RemainingMinutes, "FREE+RUNNING+有expires_at 必须 RemainingMinutes 非空")
	assert.Greater(t, *vo.RemainingMinutes, 100, "应≈120 分钟")
	assert.Equal(t, "running", vo.Status)
	assert.Equal(t, "FREE", vo.Plan)
}

func TestToVOPaidNoExpiryKeepsNilRemainingMinutes(t *testing.T) {
	// PAID_RESIDENT 无 expires_at → RemainingMinutes nil → 前端显示 "Unlimited"
	inst := &service.HermesInstance{
		InstanceID: "i2",
		Status:     "RUNNING",
		Plan:       "PAID_RESIDENT",
		ExpiresAt:  "", // 付费无限制
	}
	vo := toVO(inst)
	assert.Nil(t, vo.RemainingMinutes, "PAID 无 expires_at 应 RemainingMinutes=nil → 前端 Unlimited")
}

func TestToVOExpiredFreeReturnsNilOrZero(t *testing.T) {
	// 乙: FREE 但已过期 → remainingMinutes 应返 nil(或 0)。当前实现:
	// remainingMinutes() 见 hermes.go:66+,需读它确认过期返 nil 还是负数
	// 此测先断言"不返负分钟" — 前端不应显示"-5 minutes"
	past := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	inst := &service.HermesInstance{
		InstanceID: "i3",
		Status:     "RUNNING",
		Plan:       "FREE",
		ExpiresAt:  past,
	}
	vo := toVO(inst)
	if vo.RemainingMinutes != nil {
		assert.GreaterOrEqual(t, *vo.RemainingMinutes, 0, "过期不应显示负分钟")
	}
}
