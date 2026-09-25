package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// x402TestSetup 建表 + 一个可入账的普通用户。TestMain 的 AutoMigrate 列表不含
// X402 表,故在测试内补建(幂等)。
func x402TestSetup(t *testing.T, username string) *User {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&X402Hold{}, &X402AgentBind{}))
	user := &User{Username: username, Password: "x", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, AffCode: "aff_" + username}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		DB.Where("1 = 1").Delete(&X402Hold{})
		DB.Where("1 = 1").Delete(&X402AgentBind{})
		DB.Unscoped().Delete(user)
	})
	return user
}

func newTestHold(tradeNo string, money float64) *X402Hold {
	h := &X402Hold{
		TradeNo:    tradeNo,
		ClaimToken: "claim_" + tradeNo,
		Amount:     int64(money),
		Money:      money,
		Status:     X402HoldStatusPending,
		CreatedAt:  GetDBTimestamp(),
		ExpiresAt:  GetDBTimestamp() + X402HoldTTLSeconds,
	}
	return h
}

func TestX402HoldMarkHeldIsIdempotent(t *testing.T) {
	x402TestSetup(t, "x402mark")
	h := newTestHold("WX402_MARK1", 5)
	require.NoError(t, InsertX402Hold(h))

	require.NoError(t, MarkX402Held(h))
	assert.Equal(t, X402HoldStatusHeld, h.Status)
	assert.Greater(t, h.PayTime, int64(0))

	// 二次标记(异步 notify 与 ⑨ 步查单可能都走到这里)不得改写 pay_time
	first := h.PayTime
	require.NoError(t, MarkX402Held(h))
	assert.Equal(t, first, h.PayTime)
}

func TestX402HoldPendingExpireOnRead(t *testing.T) {
	x402TestSetup(t, "x402expire")
	h := newTestHold("WX402_EXP1", 5)
	h.ExpiresAt = GetDBTimestamp() - 1
	require.NoError(t, InsertX402Hold(h))

	got := GetX402HoldByTradeNo(h.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, X402HoldStatusExpired, got.Status)
}

func TestX402BindCreditsOnlyOnce(t *testing.T) {
	user := x402TestSetup(t, "x402credit")
	h := newTestHold("WX402_CRED1", 3)
	require.NoError(t, InsertX402Hold(h))
	require.NoError(t, MarkX402Held(h))

	before := user.Quota
	credited, err := BindX402Hold(h, user.Id, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, credited)

	want := h.Amount * int64(common.QuotaPerUnit)
	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	assert.Equal(t, before+int(want), after.Quota)

	// 台账:补建 success topup,一条,归属正确
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ? AND user_id = ?", h.TradeNo, user.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	// 重复认领:幂等命中,绝不再加钱
	credited2, err2 := BindX402Hold(&X402Hold{Id: h.Id, TradeNo: h.TradeNo, Status: X402HoldStatusCredited}, user.Id, "127.0.0.1")
	require.NoError(t, err2)
	assert.False(t, credited2)
	var after2 User
	require.NoError(t, DB.First(&after2, user.Id).Error)
	assert.Equal(t, after.Quota, after2.Quota)
}

func TestX402MarkHeldRevivesExpiredHold(t *testing.T) {
	user := x402TestSetup(t, "x402revive")
	h := newTestHold("WX402_REVIVE1", 5)
	h.ExpiresAt = GetDBTimestamp() - 1
	require.NoError(t, InsertX402Hold(h))

	// 读时过期:pending → expired(此时钱还没确认到账)
	got := GetX402HoldByTradeNo(h.TradeNo)
	require.NotNil(t, got)
	require.Equal(t, X402HoldStatusExpired, got.Status)

	// 之后微信确认扣款(notify 或查单 SUCCESS)→ 必须能复活入队并认领,否则钱悬空
	require.NoError(t, MarkX402Held(got))
	assert.Equal(t, X402HoldStatusHeld, got.Status)
	assert.Greater(t, got.PayTime, int64(0))

	credited, err := BindX402Hold(got, user.Id, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, credited)
	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	assert.Greater(t, after.Quota, 0)
}

func TestX402BindRejectsUnpaidStates(t *testing.T) {
	user := x402TestSetup(t, "x402reject")

	pending := newTestHold("WX402_PEND1", 2)
	require.NoError(t, InsertX402Hold(pending))
	credited, err := BindX402Hold(pending, user.Id, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, credited, "未扣款的单不允许认领入账")
	assert.Equal(t, X402HoldStatusPending, pending.Status)

	expired := newTestHold("WX402_EXP2", 2)
	expired.Status = X402HoldStatusExpired
	require.NoError(t, InsertX402Hold(expired))
	credited, err = BindX402Hold(expired, user.Id, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, credited)
}

func TestX402AgentBindLookupAndFirstWriteWins(t *testing.T) {
	u1 := x402TestSetup(t, "x402bind1")
	u2 := x402TestSetup(t, "x402bind2")

	assert.Nil(t, GetUserByX402AgentId("agent-x"))

	require.NoError(t, BindX402AgentId("agent-x", u1.Id))
	got := GetUserByX402AgentId("agent-x")
	require.NotNil(t, got)
	assert.Equal(t, u1.Id, got.Id)

	// 同一账号重复绑定:无副作用
	require.NoError(t, BindX402AgentId("agent-x", u1.Id))
	assert.Equal(t, u1.Id, GetUserByX402AgentId("agent-x").Id)

	// 他人抢占:保持首绑归属
	require.NoError(t, BindX402AgentId("agent-x", u2.Id))
	assert.Equal(t, u1.Id, GetUserByX402AgentId("agent-x").Id)

	// 封禁账号不再命中即时入账,钱继续挂账
	require.NoError(t, DB.Model(&User{}).Where("id = ?", u1.Id).Update("status", common.UserStatusDisabled).Error)
	assert.Nil(t, GetUserByX402AgentId("agent-x"))
}

func TestX402HeldListForUser(t *testing.T) {
	u1 := x402TestSetup(t, "x402list1")
	require.NoError(t, BindX402AgentId("agent-list", u1.Id))

	h := newTestHold("WX402_LIST1", 7)
	h.AgentUserId = "agent-list"
	require.NoError(t, InsertX402Hold(h))
	require.NoError(t, MarkX402Held(h))

	list, err := ListX402HeldForUser(u1.Id)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, h.TradeNo, list[0].TradeNo)

	empty, err := ListX402HeldForUser(u1.Id + 9999)
	require.NoError(t, err)
	assert.Len(t, empty, 0)
}
