package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
)

func TestToVORunningSetsLowercaseStatusAndPlan(t *testing.T) {
	inst := &service.HermesInstance{
		InstanceID: "i1",
		Status:     "RUNNING",
		Plan:       "FREE",
		StartedAt:  "2026-07-06T00:11:50Z",
	}
	vo := toVO(inst)
	assert.Equal(t, "running", vo.Status)
	assert.Equal(t, "FREE", vo.Plan)
	assert.Equal(t, "i1", vo.ID)
	assert.Equal(t, "2026-07-06T00:11:50Z", vo.CreatedAt)
}

func TestToVONotCreatedMapsToCreating(t *testing.T) {
	// Manager returns UPPERCASE enums; NOT_CREATED normalizes to "creating"
	// so the frontend's isFirstStart (status === 'creating') works.
	inst := &service.HermesInstance{
		InstanceID: "i2",
		Status:     "NOT_CREATED",
		Plan:       "FREE",
	}
	vo := toVO(inst)
	assert.Equal(t, "creating", vo.Status)
}

func TestToVOSleepingStaysSleeping(t *testing.T) {
	inst := &service.HermesInstance{
		InstanceID: "i3",
		Status:     "SLEEPING",
		Plan:       "FREE",
	}
	vo := toVO(inst)
	assert.Equal(t, "sleeping", vo.Status)
}
