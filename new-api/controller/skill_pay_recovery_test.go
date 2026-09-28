package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

// stub 微信查单：恢复链路的全部判定都来自这里，不碰真渠道。
func stubWechatQuery(t *testing.T, fn func(tradeNo string) (*payments.Transaction, error)) {
	t.Helper()
	prev := wechatQueryOrderFn
	wechatQueryOrderFn = fn
	t.Cleanup(func() { wechatQueryOrderFn = prev })
}

func txOf(state, txID, openid string, fen int64) *payments.Transaction {
	tx := &payments.Transaction{}
	tx.TradeState = ptr(state)
	tx.TransactionId = ptr(txID)
	if openid != "" {
		tx.Payer = &payments.TransactionPayer{Openid: ptr(openid)}
	}
	if fen > 0 {
		tx.Amount = &payments.TransactionAmount{Total: ptr(fen)}
	}
	return tx
}

func ptr[T any](v T) *T { return &v }

func setSkillPayConfigured(t *testing.T) {
	t.Helper()
	prev := operation_setting.SkillPayEnabled
	origPrice, origID, origDev, origPub, origKey := operation_setting.SkillPayPriceFen, operation_setting.SkillPaySkillId,
		operation_setting.SkillPayDeveloperId, operation_setting.SkillPayPubKeyId, operation_setting.SkillPayPrivateKeyPEM
	operation_setting.SkillPayEnabled = true
	operation_setting.SkillPayPriceFen = 10
	operation_setting.SkillPaySkillId = "sk-test"
	operation_setting.SkillPayDeveloperId = "sh-test"
	operation_setting.SkillPayPubKeyId = "PUB_KEY_test"
	operation_setting.SkillPayPrivateKeyPEM = strings.Repeat("k", 16)
	t.Cleanup(func() {
		operation_setting.SkillPayEnabled = prev
		operation_setting.SkillPayPriceFen, operation_setting.SkillPaySkillId = origPrice, origID
		operation_setting.SkillPayDeveloperId, operation_setting.SkillPayPubKeyId, operation_setting.SkillPayPrivateKeyPEM = origDev, origPub, origKey
	})
}

// 真机事故复现：付款成功但 Agent 会话里 payment_code 丢了。
// 恢复入口凭建单时预下发的 claim_token 查单履约 —— 不需要 payment_code，也不需要登录态。
func TestSkillPayRecoverWithoutPaymentCode(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	setSkillPayConfigured(t)
	prevPrice, prevServer := operation_setting.Price, system_setting.ServerAddress
	operation_setting.Price = 1.0
	system_setting.ServerAddress = "https://savvy.test"
	t.Cleanup(func() { operation_setting.Price, system_setting.ServerAddress = prevPrice, prevServer })

	seedFulfillUser(t, db, 201, "openid-recover-user")
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_R1", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPending,
		MoneyYuan: 5, ClaimToken: "11112222333344445555666677778888",
	}).Error)
	stubWechatQuery(t, func(string) (*payments.Transaction, error) {
		return txOf("SUCCESS", "txR1", "openid-recover-user", 500), nil
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/skill/recover?claim_token=11112222333344445555666677778888", nil)
	c.Request.RemoteAddr = "10.0.0.9:1"
	SkillPayRecover(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "PAID", out["code"])
	assert.Equal(t, true, out["fulfilled"])
	assert.Equal(t, "credited", out["credit_state"]) // openid 命中老客户 → 零点击直入账

	var o model.SkillPayOrder
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_R1").First(&o).Error)
	assert.True(t, o.Fulfilled)
	assert.Equal(t, "openid-recover-user", o.PayerOpenid)
	var tu model.TopUp
	require.NoError(t, db.Where("trade_no = ?", "WX402_R1").First(&tu).Error)
	assert.Equal(t, 201, tu.UserId)
	var u model.User
	require.NoError(t, db.First(&u, 201).Error)
	assert.Positive(t, u.Quota)

	// 再点一次（幂等）：不重复入账、不再查单
	stubWechatQuery(t, func(string) (*payments.Transaction, error) {
		t.Fatal("已履约的单不应再向微信查单")
		return nil, nil
	})
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/skill/recover?claim_token=11112222333344445555666677778888", nil)
	SkillPayRecover(c2)
	require.Equal(t, http.StatusOK, w2.Code)
	var out2 map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &out2))
	assert.Equal(t, true, out2["fulfilled"])
}

// 匿名付款人（openid 没命中站内用户）：恢复入口完成履约但只给认领链接，钱不凭空入任何人账户。
func TestSkillPayRecoverAnonymousPayerGetsClaim(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	setSkillPayConfigured(t)
	prevServer := system_setting.ServerAddress
	system_setting.ServerAddress = "https://savvy.test"
	t.Cleanup(func() { system_setting.ServerAddress = prevServer })

	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_R2", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPaid,
		MoneyYuan: 20, ClaimToken: "aaaabbbbccccddddeeeeffff00001111", PayerOpenid: "openid-ghost",
	}).Error)
	stubWechatQuery(t, func(string) (*payments.Transaction, error) {
		return txOf("SUCCESS", "txR2", "openid-ghost", 2000), nil
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/skill/recover?claim_token=aaaabbbbccccddddeeeeffff00001111", nil)
	SkillPayRecover(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "PAID", out["code"])
	assert.Equal(t, "need_claim", out["credit_state"])
	assert.Equal(t, "https://savvy.test/agent?claim_token=aaaabbbbccccddddeeeeffff00001111&out_trade_no=WX402_R2", out["claim_url"])
	var tu model.TopUp
	require.NoError(t, db.Where("trade_no = ?", "WX402_R2").First(&tu).Error)
	assert.Zero(t, tu.UserId)
	assert.Equal(t, common.TopUpStatusSuccess, tu.Status) // 钱已确认，挂在单上等认领
}

// 未付款时点开恢复链接：只报状态，不建 TopUp、不履约。
func TestSkillPayRecoverNotPaid(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	setSkillPayConfigured(t)
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_R3", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPending,
		MoneyYuan: 1, ClaimToken: "33334444555566667777888899990000",
	}).Error)
	stubWechatQuery(t, func(string) (*payments.Transaction, error) { return txOf("NOTPAY", "", "", 0), nil })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/skill/recover?claim_token=33334444555566667777888899990000", nil)
	SkillPayRecover(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "NOT_PAID", out["code"])
	assert.Nil(t, out["claim_url"])
	var n int64
	require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", "WX402_R3").Count(&n).Error)
	assert.Zero(t, n)
}

// 猜凭据：404，且不泄露「存在但别的渠道」这类差异。
func TestSkillPayRecoverUnknownToken(t *testing.T) {
	setupSkillPayFulfillTestDB(t)
	setSkillPayConfigured(t)
	stubWechatQuery(t, func(string) (*payments.Transaction, error) { t.Fatal("不该查单"); return nil, nil })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/skill/recover?claim_token=00000000000000000000000000000000", nil)
	SkillPayRecover(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// qa 单不参与免码恢复 —— 否则等于绕过 payment_code 白送一次 AI 回答。
func TestSkillPayRecoverRejectsQAOrder(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	setSkillPayConfigured(t)
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_R4", Kind: model.SkillPayKindQA, Status: model.SkillPayStatusPaid,
		ClaimToken: "44445555666677778888999900001111",
	}).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/skill/recover?claim_token=44445555666677778888999900001111", nil)
	SkillPayRecover(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// 兜底任务：已付款的滞留单补履约；超期未付的单关单停止轮询。
func TestSkillPaySweepFulfillsAndCloses(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	setSkillPayConfigured(t)
	prevPrice := operation_setting.Price
	operation_setting.Price = 1.0
	t.Cleanup(func() { operation_setting.Price = prevPrice })

	seedFulfillUser(t, db, 301, "openid-swept")
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_S1", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPaid,
		MoneyYuan: 30, PayerOpenid: "openid-swept", CreateTime: now - 600,
	}).Error)
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_S2", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPending,
		MoneyYuan: 50, CreateTime: now - 600, // 未付且没超 2h：不该被关单
	}).Error)
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_S3", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPending,
		MoneyYuan: 60, CreateTime: now - 3*3600, // 超期未付：关单
	}).Error)

	stubWechatQuery(t, func(no string) (*payments.Transaction, error) {
		switch no {
		case "WX402_S1":
			return txOf("SUCCESS", "txS1", "openid-swept", 3000), nil
		default:
			return txOf("NOTPAY", "", "", 0), nil
		}
	})

	orders, err := model.GetUnfulfilledSkillPayTopUpOrders(20, skillPaySweepMinAgeSec)
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, o := range orders {
		seen[o.OutTradeNo] = true
	}
	assert.True(t, seen["WX402_S1"] && seen["WX402_S2"] && seen["WX402_S3"])

	// 直接跑真实轮次（含关单分支）；user 301 没绑服务号 openid，回推自然跳过，不碰网络
	runSkillPaySweepOnce()

	// 注意：后续断言每次都用全新变量，GORM 会把已填充主键的结构体当查询条件
	var o1 model.SkillPayOrder
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_S1").First(&o1).Error)
	assert.True(t, o1.Fulfilled)
	var tu model.TopUp
	require.NoError(t, db.Where("trade_no = ?", "WX402_S1").First(&tu).Error)
	assert.Equal(t, 301, tu.UserId) // 老客户兜底直入账
	var o3 model.SkillPayOrder
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_S3").First(&o3).Error)
	assert.Equal(t, model.SkillPayStatusClosed, o3.Status)
	var o2 model.SkillPayOrder
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_S2").First(&o2).Error)
	assert.Equal(t, model.SkillPayStatusPending, o2.Status)
}

// 402 必须把认领链接带在响应体里：会话丢了，用户手上还剩这一条 URL。
func TestRespondSkillPay402CarriesClaimUrl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/skill/invoke", nil)
	respondSkillPay402(c, "code-1", "WX402_X", "0.10", "Savvy 额度充值", "https://savvy.test/agent?claim_token=abc")

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.Equal(t, "code-1", w.Header().Get("WeixinPay-Required"))
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "https://savvy.test/agent?claim_token=abc", out["claim_url"])
	assert.NotNil(t, out["claim_hint"])

	// qa 单没有认领链接，不能凭空冒出一个 key
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/skill/invoke", nil)
	respondSkillPay402(c2, "code-2", "WX402_Y", "0.10", "本次 AI 问答", "")
	var out2 map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &out2))
	_, has := out2["claim_url"]
	assert.False(t, has)
}

// 回推去重：notify 重投/多轮 sweep 只允许抢到一次推送资格。
func TestClaimSkillPayMpPushOnce(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_P2", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPaid,
	}).Error)
	assert.True(t, model.ClaimSkillPayMpPushOnce("WX402_P2"))
	assert.False(t, model.ClaimSkillPayMpPushOnce("WX402_P2"))
	assert.False(t, model.ClaimSkillPayMpPushOnce(""))
}

// 用户只拿着建单时下发的 claim_url（/agent 页轮询 status）：
// TopUp 行还不存在的瞬间，status 就地补履约，页面直接变成可认领 —— 这条桥让旧前端无需改动。
func TestAgentTopUpStatusSelfHealsSkillPay(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	setSkillPayConfigured(t)
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_R5", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPaid,
		MoneyYuan: 8, ClaimToken: "55556666777788889999000011112222", PayerOpenid: "openid-link-only",
		CreateTime: time.Now().Unix() - 60,
	}).Error)
	stubWechatQuery(t, func(string) (*payments.Transaction, error) {
		return txOf("SUCCESS", "txR5", "openid-link-only", 800), nil
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/agent/topup/status?claim_token=55556666777788889999000011112222", nil)
	c.Request.RemoteAddr = "10.0.0.8:1"
	AgentTopUpStatus(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Message string `json:"message"`
		Data    struct {
			Status  string `json:"status"`
			Claimed bool   `json:"claimed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "success", out.Message)
	assert.Equal(t, common.TopUpStatusSuccess, out.Data.Status) // 匿名单：钱已确认
	assert.False(t, out.Data.Claimed)                           // 还没人认领
	var o model.SkillPayOrder
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_R5").First(&o).Error)
	assert.True(t, o.Fulfilled)
}

// 建单限流只数「会真下一单」的首请求：带 X-Out-Trade-No 的重试是付款后的正常动作，不能被卡。
func TestSkillPayOrderCreateLimiter(t *testing.T) {
	ip := "127.0.0.2"
	for i := 0; i < 30; i++ {
		require.True(t, allowSkillPayOrderCreate(ip), "第 %d 次不该被限", i+1)
	}
	assert.False(t, allowSkillPayOrderCreate(ip))         // 超阈值即拒
	assert.True(t, allowSkillPayOrderCreate("127.0.0.3")) // 按 IP 分桶，不互相牵连

	// 限流打满时首请求确实拿到 429（而不是继续向微信下单）
	setSkillPayConfigured(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/skill/invoke", strings.NewReader(`{"action":"topup","amount_yuan":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.RemoteAddr = ip + ":1234"
	SkillInvoke(c)
	assert.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
}
