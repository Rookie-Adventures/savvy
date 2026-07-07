package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ponytail: payment_method_guard_test.go 已覆盖 stripe/creem/waffo 的 mismatch,但
// alipay/wechat 直连两条链(订阅 + topup)未单独覆盖。本文件补齐 alipay<->wechat 双向:
//   - Gate1 (anti-cross-gateway): 订阅 + topup 双链的 provider mismatch 拒绝
//   - Gate2 (idempotency): already-success 订单不再升级;topup Status guard 拒重放
// 复用 payment_method_guard_test.go 的 seed helper(insertUser/Plan/Order/TopUp)与 truncateTables。
//
// ponytail: model TestMain 设 SetMaxOpenConns(1)。CompleteSubscriptionOrder 的正向完成路径
// (调 CreateUserSubscriptionFromPlanTx → GetDBTimestamp() 开新 DB 连接)在单连接下死锁。
// 故正向完成 + 重放幂等测试放 controller 包(其 test DB 无 SetMaxOpenConns 限制)。
// 本文件只覆盖 mismatch 早退(不触达 CreateUserSubscriptionFromPlanTx)+ already-success 早退
// (在 GetSubscriptionPlanById 之前 short-circuit,不触达 GetDBTimestamp)。

// ---- Gate 1: anti-cross-gateway (alipay <-> wechat) ----

// 订阅链:订单 provider=alipay,伪造 wechat 回调 → 必拒;反向亦然。
// 锁 model/subscription.go:571-572 `if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider { return ErrPaymentMethodMismatch }`。
func TestCompleteSubscriptionOrder_RejectsCrossGatewayAlipayVsWechat(t *testing.T) {
	cases := []struct {
		name            string
		orderProvider   string // 订单创建时的 provider(模拟用户真实付款渠道)
		orderMethod     string
		notifyProvider  string // 回调声称的 provider(可能伪造)
		notifyMethod    string
		wantStatusAfter string
	}{
		{
			name:            "alipay order rejects forged wechat notify",
			orderProvider:   PaymentProviderAlipay,
			orderMethod:     PaymentMethodAlipay,
			notifyProvider:  PaymentProviderWechat,
			notifyMethod:    PaymentMethodWechat,
			wantStatusAfter: common.TopUpStatusPending,
		},
		{
			name:            "wechat order rejects forged alipay notify",
			orderProvider:   PaymentProviderWechat,
			orderMethod:     PaymentMethodWechat,
			notifyProvider:  PaymentProviderAlipay,
			notifyMethod:    PaymentMethodAlipay,
			wantStatusAfter: common.TopUpStatusPending,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 701, 0)
			plan := insertSubscriptionPlanForPaymentGuardTest(t, 711)
			tradeNo := "sub-xgw-" + tc.orderProvider
			insertSubscriptionOrderForPaymentGuardTest(t, tradeNo, 701, plan.Id, tc.orderProvider)
			// 订阅订单的 PaymentMethod 也对齐 provider(模拟真实下单)
			order := GetSubscriptionOrderByTradeNo(tradeNo)
			require.NotNil(t, order)
			order.PaymentMethod = tc.orderMethod
			require.NoError(t, order.Update())

			err := CompleteSubscriptionOrder(tradeNo, `{"forged":true}`, tc.notifyProvider, tc.notifyMethod)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)

			after := GetSubscriptionOrderByTradeNo(tradeNo)
			require.NotNil(t, after)
			assert.Equal(t, tc.wantStatusAfter, after.Status, "order status must NOT bump to success on mismatch")
			assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 701), "no UserSubscription created on mismatch")
			assert.Nil(t, GetTopUpByTradeNo(tradeNo), "no topup upserted on mismatch")
		})
	}
}

// topup 链:UpdatePendingTopUpStatus 的 provider 校验(alipay 订单拒绝 wechat expire/failed,反之亦然)。
// 这是 topup_alipay.go:78 / topup_wechat.go:82 失败清理路径调的 gate。
// 锁 model/topup.go:101-103 `if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider { return ErrPaymentMethodMismatch }`。
func TestUpdatePendingTopUpStatus_RejectsCrossGatewayAlipayVsWechat(t *testing.T) {
	cases := []struct {
		name           string
		orderProvider  string
		notifyProvider string
		targetStatus   string
	}{
		{"alipay order rejects wechat expire", PaymentProviderAlipay, PaymentProviderWechat, common.TopUpStatusExpired},
		{"alipay order rejects wechat failed", PaymentProviderAlipay, PaymentProviderWechat, common.TopUpStatusFailed},
		{"wechat order rejects alipay expire", PaymentProviderWechat, PaymentProviderAlipay, common.TopUpStatusExpired},
		{"wechat order rejects alipay failed", PaymentProviderWechat, PaymentProviderAlipay, common.TopUpStatusFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 741, 0)
			tradeNo := "top-xgw-" + tc.orderProvider + "-" + tc.targetStatus
			insertTopUpForPaymentGuardTest(t, tradeNo, 741, tc.orderProvider)

			err := UpdatePendingTopUpStatus(tradeNo, tc.notifyProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tradeNo),
				"topup status must NOT change on provider mismatch")
		})
	}
}

// ---- Gate 2: idempotency (anti-replay) ----

// 订阅幂等:订单已 Success → 再调 CompleteSubscriptionOrder 必 short-circuit 返 nil,不创建第二条 UserSubscription。
// 锁 model/subscription.go:574-576 `if order.Status == common.TopUpStatusSuccess { return nil }`(在 GetSubscriptionPlanById/CreateUserSubscriptionFromPlanTx 之前)。
// ponytail: 此测试不触达 CreateUserSubscriptionFromPlanTx(Status 早退),故无单连接死锁问题。
// 正向完成 + 重放(需触达 CreateUserSubscriptionFromPlanTx)的幂等测试见 controller 包 payment_safety_gates_test.go。
func TestCompleteSubscriptionOrder_NoUpgradeWhenAlreadySuccess(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 781, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 791)
	tradeNo := "sub-already-success-wechat"
	insertSubscriptionOrderForPaymentGuardTest(t, tradeNo, 781, plan.Id, PaymentProviderWechat)
	// 直接置 Success(模拟上一轮回调已完成)
	order := GetSubscriptionOrderByTradeNo(tradeNo)
	require.NotNil(t, order)
	order.Status = common.TopUpStatusSuccess
	require.NoError(t, order.Update())

	err := CompleteSubscriptionOrder(tradeNo, `{"replay":true}`, PaymentProviderWechat, PaymentMethodWechat)
	require.NoError(t, err, "already-success order must return nil, not error")
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 781),
		"already-success order must NOT create UserSubscription — idempotency gate must short-circuit")
}

// topup 幂等:模拟 handler 的 finalize 序列(Status→Success + IncreaseUserQuota)重放 →
// 额度仅增加一次。锁 topup_alipay.go:121 / topup_wechat.go:105 的 `Status != Pending → return` 早退。
// ponytail: handler finalize 逻辑内联在 controller 里无导出 seam,这里在 model 层复刻同一不变量:
// 第二次 UpdatePendingTopUpStatus 必被 Status guard 拒(ErrTopUpStatusInvalid),从而 handler 不会二次加额度。
func TestTopUpFinalize_IdempotentStatusGuardBlocksReplay(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 801, 0)
	tradeNo := "top-replay-alipay"
	insertTopUpForPaymentGuardTest(t, tradeNo, 801, PaymentProviderAlipay)
	// 先注入额度,模拟 handler 第一次成功完成的 IncreaseUserQuota。
	// ponytail: 用 UpdatePendingTopUpStatus 走 Status→Success(同 handler 落库路径)。
	require.NoError(t, UpdatePendingTopUpStatus(tradeNo, PaymentProviderAlipay, common.TopUpStatusSuccess))
	require.NoError(t, IncreaseUserQuota(801, 1000, true))
	quotaAfterFirst := getUserQuotaForPaymentGuardTest(t, 801)
	require.Equal(t, 1000, quotaAfterFirst)

	// 重放:再次 Status→Success 必被 Status guard 拒(订单已非 Pending)。
	err := UpdatePendingTopUpStatus(tradeNo, PaymentProviderAlipay, common.TopUpStatusSuccess)
	require.ErrorIs(t, err, ErrTopUpStatusInvalid,
		"replay must be rejected by Status guard — this is the gate handler relies on to skip re-IncreaseUserQuota")

	// 模拟 handler 见 ErrTopUpStatusInvalid → 不调 IncreaseUserQuota → 额度不变。
	quotaAfterReplay := getUserQuotaForPaymentGuardTest(t, 801)
	assert.Equal(t, quotaAfterFirst, quotaAfterReplay, "quota must NOT increase on replay")
}
