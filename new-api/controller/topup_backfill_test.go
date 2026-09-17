package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

func strPtr(s string) *string { return &s }

func TestWechatAuditFromTx_Success(t *testing.T) {
	tx := &payments.Transaction{
		TradeState:    strPtr("SUCCESS"),
		TransactionId: strPtr("4200002376202609173123456789"),
		Payer:         &payments.TransactionPayer{Openid: strPtr("oX-8k5abc")},
		SuccessTime:   strPtr("2026-09-17T14:30:40+08:00"),
	}
	audit, err := wechatAuditFromTx(tx)
	require.NoError(t, err)
	assert.Equal(t, "4200002376202609173123456789", audit.ChannelTradeNo)
	assert.Equal(t, "oX-8k5abc", audit.PayerId)
	assert.Greater(t, audit.ChannelPayTime, int64(0))
}

func TestWechatAuditFromTx_NotSuccess(t *testing.T) {
	tx := &payments.Transaction{TradeState: strPtr("CLOSED")}
	_, err := wechatAuditFromTx(tx)
	assert.Error(t, err)
}

func TestWechatAuditFromTx_Nil(t *testing.T) {
	_, err := wechatAuditFromTx(nil)
	assert.Error(t, err)
}

// handler 集成测试:stub 微信查单,验证扫描→回填→汇总全链路。
func TestBackfillTopUpChannelAudit_Handler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.User{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "u1", Email: "u1@x.com", Status: common.UserStatusEnabled}).Error)
	// 两笔待回填微信单 + 一笔已有渠道号的单(不应被扫描)
	for _, tradeNo := range []string{"WX-A", "WX-B"} {
		require.NoError(t, model.DB.Create(&model.TopUp{
			UserId: 1, Amount: 1, Money: 1, TradeNo: tradeNo,
			PaymentMethod: model.PaymentMethodWechat, PaymentProvider: model.PaymentProviderWechat,
			CreateTime: common.GetTimestamp(), Status: common.TopUpStatusSuccess,
		}).Error)
	}
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 1, Amount: 2, Money: 2, TradeNo: "WX-DONE",
		PaymentMethod: model.PaymentMethodWechat, PaymentProvider: model.PaymentProviderWechat,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusSuccess,
		ChannelTradeNo: "4200EXISTING",
	}).Error)

	origWx := wechatQueryOrderFn
	// WX-A 成功,wx-B 查单失败(覆盖 failed 分支)
	wechatQueryOrderFn = func(tradeNo string) (*payments.Transaction, error) {
		if tradeNo == "WX-B" {
			return &payments.Transaction{TradeState: strPtr("CLOSED")}, nil
		}
		return &payments.Transaction{
			TradeState:    strPtr("SUCCESS"),
			TransactionId: strPtr("4200" + tradeNo),
			Payer:         &payments.TransactionPayer{Openid: strPtr("openid-" + tradeNo)},
			SuccessTime:   strPtr("2026-09-17T14:30:40+08:00"),
		}, nil
	}
	t.Cleanup(func() { wechatQueryOrderFn = origWx })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/topup/backfill_channel", bytes.NewReader([]byte("{}")))

	BackfillTopUpChannelAudit(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Message string `json:"message"`
		Data    struct {
			Scanned int `json:"scanned"`
			Updated int `json:"updated"`
			Failed  []struct {
				TradeNo string `json:"trade_no"`
				Reason  string `json:"reason"`
			} `json:"failed"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "success", resp.Message)
	assert.Equal(t, 2, resp.Data.Scanned, "只扫描缺渠道号的成功单")
	assert.Equal(t, 1, resp.Data.Updated)
	require.Len(t, resp.Data.Failed, 1)
	assert.Equal(t, "WX-B", resp.Data.Failed[0].TradeNo)

	// WX-A 落库断言
	got := model.GetTopUpByTradeNo("WX-A")
	require.NotNil(t, got)
	assert.Equal(t, "4200WX-A", got.ChannelTradeNo)
	assert.Equal(t, "openid-WX-A", got.PayerId)
	assert.Equal(t, "u1", got.CreditedUsername)
	assert.Equal(t, "u1@x.com", got.CreditedEmail)
	assert.Greater(t, got.ChannelPayTime, int64(0))
	assert.Equal(t, got.ChannelPayTime, got.CompleteTime, "complete_time=0 的老单用渠道支付时间补齐")
	// WX-DONE 未被触碰
	assert.Equal(t, "4200EXISTING", model.GetTopUpByTradeNo("WX-DONE").ChannelTradeNo)
}

// 幂等:已有渠道号的订单不会被扫描重复回填(二次触发 scanned 不含它)。
func TestBackfillTopUpChannelAudit_Idempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.User{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "u1", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 1, Amount: 1, Money: 1, TradeNo: "WX-IDEM",
		PaymentMethod: model.PaymentMethodWechat, PaymentProvider: model.PaymentProviderWechat,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusSuccess,
		ChannelTradeNo: "4200IDEM",
	}).Error)

	origWx := wechatQueryOrderFn
	wechatQueryOrderFn = func(tradeNo string) (*payments.Transaction, error) {
		t.Fatal("已有渠道号的订单不应触发查单")
		return nil, nil
	}
	t.Cleanup(func() { wechatQueryOrderFn = origWx })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/topup/backfill_channel", bytes.NewReader([]byte("{}")))
	BackfillTopUpChannelAudit(c)

	var resp struct {
		Data struct {
			Scanned int `json:"scanned"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Zero(t, resp.Data.Scanned)
}

// 生产形态回归:AutoMigrate 加列后老订单 channel_trade_no 为 NULL(非空串),必须同样被扫描。
func TestBackfillTopUpChannelAudit_NullChannelColumn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.User{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "u1", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 1, Amount: 310, Money: 310, TradeNo: "WX-NULLCOL",
		PaymentMethod: model.PaymentMethodWechat, PaymentProvider: model.PaymentProviderWechat,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusSuccess,
	}).Error)
	// 模拟老库加列:置为 NULL
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("trade_no = ?", "WX-NULLCOL").Update("channel_trade_no", nil).Error)

	origWx := wechatQueryOrderFn
	wechatQueryOrderFn = func(tradeNo string) (*payments.Transaction, error) {
		return &payments.Transaction{
			TradeState:    strPtr("SUCCESS"),
			TransactionId: strPtr("4200NULLCOL"),
			Payer:         &payments.TransactionPayer{Openid: strPtr("openid-null")},
		}, nil
	}
	t.Cleanup(func() { wechatQueryOrderFn = origWx })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/topup/backfill_channel", bytes.NewReader([]byte("{}")))
	BackfillTopUpChannelAudit(c)

	var resp struct {
		Data struct {
			Scanned int `json:"scanned"`
			Updated int `json:"updated"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 1, resp.Data.Scanned, "NULL 列必须被扫描到")
	assert.Equal(t, 1, resp.Data.Updated)
	got := model.GetTopUpByTradeNo("WX-NULLCOL")
	require.NotNil(t, got)
	assert.Equal(t, "4200NULLCOL", got.ChannelTradeNo)
}
