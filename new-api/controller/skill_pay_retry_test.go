package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

// 官方改造模板 @user_4894573e/skill-paid v3.1.1 的「场景二：支付后重试」只带 X-Out-Trade-No
// （scripts/templates_builtin.py handle_invoke），不带 WeixinPay-Required。
// 我们原先把付款码当门票 → 照模板生成的第三方 agent 付了钱也取不到货。
// 现在无码不拒，但幂等缓存只对「码校过」或「订单已标记 paid」开放，其余一律实查渠道。

func newRetryCtx(outTradeNo, paymentCode string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/skill/invoke", nil)
	c.Request.RemoteAddr = "10.0.0.1:1234"
	c.Request.Header.Set("X-Out-Trade-No", outTradeNo)
	if paymentCode != "" {
		c.Request.Header.Set("WeixinPay-Required", paymentCode)
	}
	return c, w
}

func TestSkillPayRetryWithoutPaymentCode(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	prevPrice, prevServer := operation_setting.Price, system_setting.ServerAddress
	operation_setting.Price = 1.0
	system_setting.ServerAddress = "https://savvy.test"
	t.Cleanup(func() {
		operation_setting.Price, system_setting.ServerAddress = prevPrice, prevServer
	})

	mk := func(no, status string, fulfilled bool) {
		require.NoError(t, db.Create(&model.SkillPayOrder{
			OutTradeNo: no, PaymentCode: "PC-secret-code", Kind: model.SkillPayKindTopUp,
			MoneyYuan: 0.1, Status: status, Fulfilled: fulfilled, Content: "缓存的付费内容",
			ClaimToken: "claim-" + no[len(no)-4:], CreateTime: time.Now().Unix(),
		}).Error)
	}

	t.Run("无码且未支付：实查渠道并回 402，既不 401 也不吐缓存", func(t *testing.T) {
		mk("WX402_20260929010101AAAAAAAAAAAA", model.SkillPayStatusPending, true)
		queried := false
		stubWechatQuery(t, func(string) (*payments.Transaction, error) {
			queried = true
			return txOf("NOTPAY", "", "", 0), nil
		})
		c, w := newRetryCtx("WX402_20260929010101AAAAAAAAAAAA", "")
		handleSkillPayRetry(c, SkillInvokeRequest{}, "WX402_20260929010101AAAAAAAAAAAA")

		assert.True(t, queried, "无码重试必须实查渠道")
		assert.Equal(t, http.StatusPaymentRequired, w.Code, w.Body.String())
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "PAYMENT_NOT_COMPLETED", body["code"])
		assert.NotEqualValues(t, "缓存的付费内容", body["content"], "拿到订单号不等于付过钱，缓存不能命中")
	})

	t.Run("无码且渠道已 SUCCESS：正常履约（第三方 agent 付完能取到货）", func(t *testing.T) {
		mk("WX402_20260929010102BBBBBBBBBBBB", model.SkillPayStatusPending, false)
		stubWechatQuery(t, func(string) (*payments.Transaction, error) {
			return txOf("SUCCESS", "wx-tx-2", "openid-stranger", 10), nil
		})
		c, w := newRetryCtx("WX402_20260929010102BBBBBBBBBBBB", "")
		handleSkillPayRetry(c, SkillInvokeRequest{}, "WX402_20260929010102BBBBBBBBBBBB")

		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.NotEmpty(t, body["content"])
		var order model.SkillPayOrder
		require.NoError(t, db.Where("out_trade_no = ?", "WX402_20260929010102BBBBBBBBBBBB").First(&order).Error)
		assert.Equal(t, "wx-tx-2", order.TransactionId)
	})

	t.Run("码错仍然拒绝", func(t *testing.T) {
		mk("WX402_20260929010103CCCCCCCCCCCC", model.SkillPayStatusPaid, true)
		queried := false
		stubWechatQuery(t, func(string) (*payments.Transaction, error) {
			queried = true
			return txOf("SUCCESS", "wx-tx-3", "", 10), nil
		})
		c, w := newRetryCtx("WX402_20260929010103CCCCCCCCCCCC", "PC-wrong-code")
		handleSkillPayRetry(c, SkillInvokeRequest{}, "WX402_20260929010103CCCCCCCCCCCC")

		assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		assert.False(t, queried, "码错应在查单前就拒")
	})

	t.Run("无码但订单已标记 paid：允许走幂等缓存，不再多打一次查单", func(t *testing.T) {
		mk("WX402_20260929010104DDDDDDDDDDDD", model.SkillPayStatusPaid, true)
		queried := false
		stubWechatQuery(t, func(string) (*payments.Transaction, error) {
			queried = true
			return txOf("SUCCESS", "wx-tx-4", "", 10), nil
		})
		c, w := newRetryCtx("WX402_20260929010104DDDDDDDDDDDD", "")
		handleSkillPayRetry(c, SkillInvokeRequest{}, "WX402_20260929010104DDDDDDDDDDDD")

		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.False(t, queried)
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "缓存的付费内容", body["content"])
		assert.Equal(t, true, body["already_fulfilled"])
	})
}

// 402 双通道契约与 amount 单位见 skill_pay_recovery_test.go 的 claim 用例。
