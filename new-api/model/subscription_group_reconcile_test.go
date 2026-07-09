package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedReconcileUser builds a single user with the given baseline group.
func seedReconcileUser(t *testing.T, group string) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "reconcile-user",
		Status:   1,
		Group:    group,
	}).Error)
}

// seedActiveSub builds an active, non-expired UserSubscription whose upgrade_group
// is upgradeGroup. Mirrors the drift scenario: paid plan committed but the
// activation write to users.group never landed.
func seedActiveSub(t *testing.T, upgradeGroup string) {
	t.Helper()
	require.NoError(t, DB.Create(&SubscriptionPlan{
		Id:            300,
		Title:         "Pro Plan",
		PriceAmount:   0.05,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   50,
		UpgradeGroup:  upgradeGroup,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&UserSubscription{
		UserId:         1,
		PlanId:         300,
		AmountTotal:    50,
		StartTime:      now - 3600,
		EndTime:        now + 86400, // well in the future: active, not expired
		Status:         "active",
		Source:         "balance",
		UpgradeGroup:   upgradeGroup,
		DowngradeGroup: "default",
	}).Error)
}

func reloadUserGroup(t *testing.T) string {
	t.Helper()
	var g string
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Select(commonGroupCol).Find(&g).Error)
	return g
}

// TestReconcileUserGroupHealsDriftFromDefault: a user stuck at "default" while an
// active pro subscription exists is the exact drift symptom. reconcile must write
// "pro" back to DB and return it.
func TestReconcileUserGroupHealsDriftFromDefault(t *testing.T) {
	truncateTables(t)
	seedReconcileUser(t, "default")
	seedActiveSub(t, "pro")

	got := reconcileUserGroupIfStale(1, "default")
	assert.Equal(t, "pro", got)
	assert.Equal(t, "pro", reloadUserGroup(t))
}

// TestReconcileUserGroupElevatedShortCircuits: a user already on a paid group must
// never be touched — no sub query logic that could accidentally downgrade on the
// read path. Returns the elevated group unchanged.
func TestReconcileUserGroupElevatedShortCircuits(t *testing.T) {
	truncateTables(t)
	seedReconcileUser(t, "pro")
	// No active sub seeded on purpose; even if one existed it wouldn't run.
	got := reconcileUserGroupIfStale(1, "pro")
	assert.Equal(t, "pro", got)
	assert.Equal(t, "pro", reloadUserGroup(t))
}

// TestReconcileUserGroupNoActiveSubKeepsDefault: baseline group with no active
// upgrade subscription stays baseline — a genuinely free user must not be
// elevated into a phantom plan.
func TestReconcileUserGroupNoActiveSubKeepsDefault(t *testing.T) {
	truncateTables(t)
	seedReconcileUser(t, "default")
	// No subscription at all.
	got := reconcileUserGroupIfStale(1, "default")
	assert.Equal(t, "default", got)
	assert.Equal(t, "default", reloadUserGroup(t))
}
