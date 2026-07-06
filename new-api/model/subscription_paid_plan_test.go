package model

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpgradeCalledAfterSubscriptionOrder verifies that a successful
// CompleteSubscriptionOrder triggers a manager upgrade call for an active
// instance.
func TestUpgradeCalledAfterSubscriptionOrder(t *testing.T) {
	truncateTables(t)

	upgraded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/users/1/instance" {
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","user_id":"1","status":"RUNNING","plan":"FREE"}}`))
			return
		}
		if r.URL.Path == "/internal/instances/inst-1/upgrade" {
			upgraded = true
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","status":"RUNNING","plan":"STARTER","needs_upgrade":false}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	t.Setenv("HERMES_MANAGER_URL", server.URL)
	t.Setenv("SAVVY_HMAC_SECRET", "test-secret")

	// The model-layer hook is a package var; production wires it to the real
	// service call. The test overrides it with a thin closure that drives the
	// same httptest manager so we assert the hook fires post-commit.
	upgradeStub := newManagerHttpStub(t, server.URL)
	prevUpgrade := NotifyManagerUpgradeFn
	NotifyManagerUpgradeFn = upgradeStub.upgrade
	t.Cleanup(func() { NotifyManagerUpgradeFn = prevUpgrade })

	setupPaidPlanOrderFixtures(t)
	err := CompleteSubscriptionOrder("trade-upgrade-test", `{"provider":"test"}`, "", "")
	require.NoError(t, err)
	upgradeStub.wait(t)
	assert.True(t, upgraded, "manager upgrade should be called after subscription completion")
}

// TestDowngradeCalledAfterExpiry verifies ExpireDueSubscriptions triggers a
// manager downgrade call.
func TestDowngradeCalledAfterExpiry(t *testing.T) {
	truncateTables(t)

	downgraded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/users/1/instance" {
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","user_id":"1","status":"RUNNING","plan":"STARTER"}}`))
			return
		}
		if r.URL.Path == "/internal/instances/inst-1/downgrade" {
			downgraded = true
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","status":"RUNNING","plan":"FREE","expires_at":"2026-07-06T16:00:00Z"}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	t.Setenv("HERMES_MANAGER_URL", server.URL)
	t.Setenv("SAVVY_HMAC_SECRET", "test-secret")

	downgradeStub := newManagerHttpStub(t, server.URL)
	prevDowngrade := NotifyManagerDowngradeFn
	NotifyManagerDowngradeFn = downgradeStub.downgrade
	t.Cleanup(func() { NotifyManagerDowngradeFn = prevDowngrade })

	setupExpiredSubscriptionFixture(t)
	_, err := ExpireDueSubscriptions(100)
	require.NoError(t, err)
	downgradeStub.wait(t)
	assert.True(t, downgraded, "manager downgrade should be called after expiry")
}

// managerHttpStub drives the real hermes-manager HTTP surface (GET instance,
// POST upgrade/downgrade) so model-layer tests can assert the hook fired
// without importing the service package (avoids model<->service cycle).
type managerHttpStub struct {
	done chan struct{}
}

func newManagerHttpStub(t *testing.T, baseURL string) *managerHttpStub {
	t.Helper()
	s := &managerHttpStub{done: make(chan struct{})}
	// fire-and-forget; wait() drains once. Reusable for a single notify call.
	return s
}

// upgrade mirrors service.NotifyManagerUpgrade's contract: GET instance, then
// POST upgrade. Errors are swallowed because the model caller only SYSLOGs.
func (s *managerHttpStub) upgrade(userID int, group string) error {
	defer close(s.done)
	inst, err := managerGetRunningInstance(userID)
	if err != nil || inst == nil || inst.Status != "RUNNING" {
		return nil
	}
	res, ok := managerPlanResources(group)
	if !ok {
		return nil
	}
	planName, ok := managerGroupToPlanName(group)
	if !ok {
		return nil
	}
	return managerPostUpgrade(userID, inst.InstanceID, planName, res)
}

func (s *managerHttpStub) downgrade(userID int) error {
	defer close(s.done)
	inst, err := managerGetRunningInstance(userID)
	if err != nil || inst == nil || inst.Status != "RUNNING" {
		return nil
	}
	return managerPostDowngrade(userID, inst.InstanceID, time.Now().Add(2*time.Hour))
}

func (s *managerHttpStub) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("manager notify hook did not fire within 2s")
	}
}

// setupPaidPlanOrderFixtures seeds a user, a starter plan, and a pending order.
// CompleteSubscriptionOrder is called with empty expectedPaymentProvider /
// actualPaymentMethod, so the payment-method guard is skipped and the order
// completes regardless of the stored PaymentProvider value.
func setupPaidPlanOrderFixtures(t *testing.T) {
	t.Helper()

	user := &User{
		Id:       1,
		Username: "upgrade-user",
		Status:   common.UserStatusEnabled,
		Quota:    0,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)

	plan := &SubscriptionPlan{
		Id:            100,
		Title:         "Starter Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
		UpgradeGroup:  "starter",
	}
	require.NoError(t, DB.Create(plan).Error)

	order := &SubscriptionOrder{
		UserId:          1,
		PlanId:          plan.Id,
		Money:           9.99,
		TradeNo:         "trade-upgrade-test",
		PaymentMethod:   "test",
		PaymentProvider: "test",
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
}

// setupExpiredSubscriptionFixture seeds a user + an active UserSubscription
// whose end_time is in the past so ExpireDueSubscriptions picks it up. It also
// sets DowngradeGroup so the downgrade branch runs and the notify hook fires.
func setupExpiredSubscriptionFixture(t *testing.T) {
	t.Helper()

	user := &User{
		Id:       1,
		Username: "expire-user",
		Status:   common.UserStatusEnabled,
		Quota:    0,
		Group:    "starter",
	}
	require.NoError(t, DB.Create(user).Error)

	plan := &SubscriptionPlan{
		Id:             200,
		Title:          "Starter Plan",
		PriceAmount:    9.99,
		Currency:       "USD",
		DurationUnit:   SubscriptionDurationMonth,
		DurationValue:  1,
		Enabled:        true,
		TotalAmount:    1000,
		UpgradeGroup:   "starter",
		DowngradeGroup: "default",
	}
	require.NoError(t, DB.Create(plan).Error)

	now := time.Now().Unix()
	sub := &UserSubscription{
		UserId:         1,
		PlanId:         plan.Id,
		AmountTotal:    1000,
		StartTime:      now - 3600,
		EndTime:        now - 60,
		Status:         "active",
		Source:         "order",
		UpgradeGroup:   "starter",
		PrevUserGroup:  "default",
		DowngradeGroup: "default",
	}
	require.NoError(t, DB.Create(sub).Error)
}
