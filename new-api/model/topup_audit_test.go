package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTopUpAuditTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &User{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM users")
	})
}

func newAuditUser(t *testing.T, quota int) *User {
	t.Helper()
	u := &User{Username: "u" + common.GetRandomString(6), Status: common.UserStatusEnabled, Quota: quota}
	require.NoError(t, u.Insert(0))
	// User.Insert 强制覆盖为新用户赠送额度,这里回写测试期望的初始余额
	require.NoError(t, DB.Model(&User{}).Where("id = ?", u.Id).Update("quota", quota).Error)
	return u
}

func newPendingTopUp(t *testing.T, userId int, tradeNo string) *TopUp {
	t.Helper()
	tu := &TopUp{UserId: userId, Amount: 10, Money: 9.9, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodAlipay, PaymentProvider: PaymentProviderAlipay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending}
	require.NoError(t, tu.Insert())
	return tu
}

func TestCompleteTopUpWithAudit_Success(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 52000000)
	newPendingTopUp(t, u.Id, "AUDIT-1")

	err := CompleteTopUpWithAudit("AUDIT-1", PaymentProviderAlipay,
		TopUpAudit{ChannelTradeNo: "2026091722001", PayerId: "2088x", ChannelPayTime: 1758090640}, nil)
	require.NoError(t, err)

	got := GetTopUpByTradeNo("AUDIT-1")
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Equal(t, "2026091722001", got.ChannelTradeNo)
	assert.Equal(t, "2088x", got.PayerId)
	assert.Equal(t, 52000000, got.BalanceBefore)
	assert.Equal(t, 52000000+10*int(common.QuotaPerUnit), got.BalanceAfter)
	assert.Equal(t, u.Username, got.CreditedUsername)
	assert.Greater(t, got.CompleteTime, int64(0))
	var after User
	require.NoError(t, DB.Where("id = ?", u.Id).First(&after).Error)
	assert.Equal(t, 52000000+10*int(common.QuotaPerUnit), after.Quota)
}

func TestCompleteTopUpWithAudit_Idempotent(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 0)
	newPendingTopUp(t, u.Id, "AUDIT-2")
	require.NoError(t, CompleteTopUpWithAudit("AUDIT-2", PaymentProviderAlipay, TopUpAudit{}, nil))
	require.NoError(t, CompleteTopUpWithAudit("AUDIT-2", PaymentProviderAlipay, TopUpAudit{}, nil))
	var after User
	require.NoError(t, DB.Where("id = ?", u.Id).First(&after).Error)
	assert.Equal(t, 10*int(common.QuotaPerUnit), after.Quota, "二次调用不得重复加钱")
}

func TestCompleteTopUpWithAudit_ProviderMismatch(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 0)
	newPendingTopUp(t, u.Id, "AUDIT-3")
	err := CompleteTopUpWithAudit("AUDIT-3", PaymentProviderWechat, TopUpAudit{}, nil)
	assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
}

func TestCompleteTopUpWithAudit_NotPending(t *testing.T) {
	setupTopUpAuditTest(t)
	u := newAuditUser(t, 0)
	tu := newPendingTopUp(t, u.Id, "AUDIT-4")
	tu.Status = common.TopUpStatusExpired
	require.NoError(t, tu.Update())
	assert.ErrorIs(t, CompleteTopUpWithAudit("AUDIT-4", PaymentProviderAlipay, TopUpAudit{}, nil), ErrTopUpStatusInvalid)
}

func TestCompleteTopUpWithAudit_MutateAndGuestOrder(t *testing.T) {
	setupTopUpAuditTest(t)
	// 游客单 user_id=0:只标记成功,不加余额
	tu := &TopUp{UserId: 0, Amount: 10, Money: 9.9, TradeNo: "AUDIT-5",
		PaymentMethod: PaymentMethodAlipay, PaymentProvider: PaymentProviderAlipayAgent,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending}
	require.NoError(t, tu.Insert())
	err := CompleteTopUpWithAudit("AUDIT-5", PaymentProviderAlipayAgent,
		TopUpAudit{ChannelTradeNo: "chan-1"}, func(tu *TopUp) { tu.Money = 9.9 })
	require.NoError(t, err)
	got := GetTopUpByTradeNo("AUDIT-5")
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Equal(t, "chan-1", got.ChannelTradeNo)
	assert.Equal(t, float64(9.9), got.Money, "mutate 回填生效")
}
