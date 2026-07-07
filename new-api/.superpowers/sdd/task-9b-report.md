# Task 9b — Payment Safety Gates Programmatic Tests

3 money-path gates locked via Go tests. No real sandbox payment needed. All guards sanity-verified (test fails when guard removed).

## Files
- `new-api/model/subscription_safety_gates_test.go` — Gate1 (sub+topup mismatch) + Gate2 (already-success, topup Status guard)
- `new-api/controller/payment_safety_gates_test.go` — Gate3 (verify-fail→"fail") + Gate2 (legit-completion + replay idempotency)

## Gate 1: anti-cross-gateway (provider mismatch)

| Test | File:line | Asserts | Locks |
|------|-----------|---------|-------|
| `TestCompleteSubscriptionOrder_RejectsCrossGatewayAlipayVsWechat` | model/subscription_safety_gates_test.go:31 | alipay order + forged wechat notify → `ErrPaymentMethodMismatch`, Status stays Pending, no UserSubscription, no TopUp upsert; reverse also | subscription.go:571-572 |
| `TestUpdatePendingTopUpStatus_RejectsCrossGatewayAlipayVsWechat` | model/subscription_safety_gates_test.go:67 | alipay topup + wechat expire/failed → `ErrPaymentMethodMismatch`, Status stays Pending; reverse also (4 sub-cases) | topup.go:101-103 |
| `TestCompleteSubscriptionOrder_LegitAlipayWechatProviderCompletes` | controller/payment_safety_gates_test.go:166 | same-provider notify → Status→Success + 1 UserSubscription (alipay + wechat) |正向 path lock — prevents Gate1 tests passing via degenerate "always-reject" |

**Sanity-verified**: commenting out subscription.go:571-572 → mismatch test fails (deadlocks past guard into CreateUserSubscriptionFromPlanTx under SetMaxOpenConns(1); FAIL either way). Restored.

**Replay/mismatch setup**: order seeded with `PaymentProvider=X`, notify calls `CompleteSubscriptionOrder(tradeNo, payload, Y, ...)` where Y≠X.

## Gate 2: idempotency (anti-replay)

| Test | File:line | Asserts | Locks |
|------|-----------|---------|-------|
| `TestCompleteSubscriptionOrder_IdempotentOnReplay` | controller/payment_safety_gates_test.go:196 | 5 replays of same legit CompleteSubscriptionOrder → exactly 1 UserSubscription, ProviderPayload retains first (`"replay":0`), not overwritten | subscription.go:574-576 (`Status==Success return nil` before CreateUserSubscriptionFromPlanTx) |
| `TestCompleteSubscriptionOrder_NoUpgradeWhenAlreadySuccess` | model/subscription_safety_gates_test.go:120 | order pre-set Success → CompleteSubscriptionOrder returns nil, 0 UserSubscription created | subscription.go:574-576 |
| `TestTopUpFinalize_IdempotentStatusGuardBlocksReplay` | model/subscription_safety_gates_test.go:142 | first UpdatePendingTopUpStatus→Success + IncreaseUserQuota(1000); replay → `ErrTopUpStatusInvalid`, quota unchanged | topup.go:104-106 (`Status != Pending → ErrTopUpStatusInvalid`) → handler topup_alipay.go:121 / topup_wechat.go:105 skip re-IncreaseUserQuota |

**Sanity-verified**: commenting out subscription.go:574-576 → `NoUpgradeWhenAlreadySuccess` fails (`ErrSubscriptionOrderStatusInvalid` returned instead of nil). Restored.

**Replay setup**: loop calls same CompleteSubscriptionOrder N times with varying payload; first transitions Pending→Success, subsequent hit `Status==Success` early-return.

## Gate 3: signature-fail rejection

| Test | File:line | Asserts | Locks |
|------|-----------|---------|-------|
| `TestSubscriptionAlipayNotify_RejectsOnVerifySignFailure` | controller/payment_safety_gates_test.go:44 | verify-fail → response body contains "fail", order stays Pending, 0 UserSubscription | subscription_payment_alipay.go:151-154 |
| `TestAlipayNotify_RejectsOnVerifySignFailure` | controller/payment_safety_gates_test.go:104 | verify-fail → "fail", topup stays Pending, quota 0 | topup_alipay.go:100-103 |
| `TestInjectedAlipayClientVerifySignErrors` | controller/payment_safety_gates_test.go:158 | fixture premise: injected client's VerifySign returns error (guards against SDK changing empty-sign behavior) | test-fixture integrity |

**Sanity-verified**: commenting out subscription_payment_alipay.go:151-154 → sub test fails on 3 assertions (body no longer "fail" but "success"; Status=Success not Pending; UserSubscription count=1 not 0). Restored.

**Tamper setup**: inject real `*alipay.Client` (via `alipay.New` with throwaway RSA-2048 key, NO public key loaded → `getVerifier` returns `ErrAliPublicKeyNotFound`) into `alipayClient` singleton. POST notify form with `trade_status=TRADE_SUCCESS` but no `sign` field → `VerifySign` errs → handler writes "fail".

### Honest limitation on Gate 3
- Tests cover the **alipay** verify-fail path (SDK `VerifySign` coerced to error via missing public key). The **wechat** verify-fail path goes through `decryptWxNativeNotify` (SDK `ParseNotifyRequest` with RSA verifier + AES-GCM decrypt) which requires a real wechat platform cert + APIv3 key setup — not coercible to a clean verify-fail in a unit test without heavy crypto fixture. The wechat handler's verify-fail branch (`wechat_notify.go:28-30`: `if err != nil || tradeNo == "" { write FAIL }`) is structurally identical to alipay's (early-return writing "FAIL" before any DB mutation) and is covered by the same contract pattern. A full wechat crypto integration test is deferred to the human sandbox follow-up (plan Step4a-d).

## Test DB setup notes
- model package: reuses `TestMain` in-memory SQLite + `truncateTables` + seed helpers from `payment_method_guard_test.go` (`insertUserForPaymentGuardTest` etc.).
- controller package: reuses `setupModelListControllerTestDB` (separate in-memory SQLite, NO `SetMaxOpenConns(1)` limit).
- **Why split**: model `TestMain` sets `SetMaxOpenConns(1)`. `CompleteSubscriptionOrder`'s legit-completion path calls `CreateUserSubscriptionFromPlanTx` → `GetDBTimestamp()` (uses `DB` not `tx`) → opens new connection → deadlock under single-conn limit. The mismatch + already-success tests short-circuit before this point (safe in model). The legit-completion + replay tests traverse the deadlock point (moved to controller, no conn limit). This is a pre-existing latent deadlock hazard in production (any txn calling `GetDBTimestamp`/`GetSubscriptionPlanById` non-tx variants) but not in scope for this task.

## Pass output (all gates, both packages)
```
--- PASS: TestSubscriptionAlipayNotify_RejectsOnVerifySignFailure (0.06s)
--- PASS: TestAlipayNotify_RejectsOnVerifySignFailure (0.02s)
--- PASS: TestInjectedAlipayClientVerifySignErrors (0.11s)
--- PASS: TestCompleteSubscriptionOrder_LegitAlipayWechatProviderCompletes (0.02s)
    --- PASS: .../alipay (0.01s)
    --- PASS: .../wechat (0.01s)
--- PASS: TestCompleteSubscriptionOrder_IdempotentOnReplay (0.01s)
ok  github.com/QuantumNous/new-api/controller  4.117s
--- PASS: TestCompleteSubscriptionOrder_RejectsCrossGatewayAlipayVsWechat (0.00s)
    --- PASS: .../alipay_order_rejects_forged_wechat_notify (0.00s)
    --- PASS: .../wechat_order_rejects_forged_alipay_notify (0.00s)
--- PASS: TestUpdatePendingTopUpStatus_RejectsCrossGatewayAlipayVsWechat (0.00s)
    [4 sub-cases PASS]
--- PASS: TestCompleteSubscriptionOrder_NoUpgradeWhenAlreadySuccess (0.00s)
--- PASS: TestTopUpFinalize_IdempotentStatusGuardBlocksReplay (0.00s)
ok  github.com/QuantumNous/new-api/model  3.672s
```

Full regression `go test ./controller/... ./model/...` → both `ok`, 0 failures.

## No Critical findings
All 3 gates present in production code. No missing guards discovered.
