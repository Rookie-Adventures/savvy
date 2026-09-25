package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupX402ControllerTest(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.X402Hold{}, &model.X402AgentBind{}, &model.TopUp{}, &model.Log{}))
	t.Cleanup(func() { wechatNativeSvc = nil })
	restoreX402Settings(t)
	operation_setting.X402Enabled = true
	operation_setting.SkillhubDeveloperId = "sh-test"
	operation_setting.SkillhubPubKeyId = "PUB_KEY_TEST"
	operation_setting.SkillhubPrivateKeyPEM = "pem"
	operation_setting.X402SkillId = "savvy-quota-topup"
	operation_setting.X402AmountCents = 100
}

func restoreX402Settings(t *testing.T) {
	t.Helper()
	s := struct {
		enabled         bool
		devID, pubKeyID string
		pem, skillID    string
		version         string
		amount          int
		serviceName     string
		mpOAuth         string
		wechatMchID     string
	}{
		operation_setting.X402Enabled, operation_setting.SkillhubDeveloperId,
		operation_setting.SkillhubPubKeyId, operation_setting.SkillhubPrivateKeyPEM,
		operation_setting.X402SkillId, operation_setting.X402SkillVersion,
		operation_setting.X402AmountCents, operation_setting.X402ServiceName,
		operation_setting.X402MpOAuthURL, operation_setting.WechatMchID,
	}
	t.Cleanup(func() {
		operation_setting.X402Enabled = s.enabled
		operation_setting.SkillhubDeveloperId = s.devID
		operation_setting.SkillhubPubKeyId = s.pubKeyID
		operation_setting.SkillhubPrivateKeyPEM = s.pem
		operation_setting.X402SkillId = s.skillID
		operation_setting.X402SkillVersion = s.version
		operation_setting.X402AmountCents = s.amount
		operation_setting.X402ServiceName = s.serviceName
		operation_setting.X402MpOAuthURL = s.mpOAuth
		operation_setting.WechatMchID = s.wechatMchID
	})
}

func newPostContext(t *testing.T, path string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"query":"ping"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c, w
}

func TestSkillInvokeFailClosedWhenUnconfigured(t *testing.T) {
	setupX402ControllerTest(t)
	operation_setting.X402Enabled = false

	c, w := newPostContext(t, "/api/x402/invoke", nil)
	SkillInvoke(c)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "X402_NOT_CONFIGURED")
	// 未配置时不得留下任何订单
	var count int64
	assert.NoError(t, model.DB.Model(&model.X402Hold{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestSkillInvokeRetryRejectsForeignTradeNo(t *testing.T) {
	setupX402ControllerTest(t)

	c, w := newPostContext(t, "/api/x402/invoke", map[string]string{"X-Out-Trade-No": "WXUSR42NO123"})
	SkillInvoke(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "BAD_OUT_TRADE_NO")
}

func TestSkillInvokeRetryUnknownOrder(t *testing.T) {
	setupX402ControllerTest(t)

	c, w := newPostContext(t, "/api/x402/invoke", map[string]string{"X-Out-Trade-No": "WX402_20200101000000ABCDEF012345"})
	SkillInvoke(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "ORDER_NOT_FOUND")
}

func TestSkillInvokeRetryRequiresMatchingPaymentCode(t *testing.T) {
	setupX402ControllerTest(t)
	hold := &model.X402Hold{
		TradeNo: "WX402_CODE000000000000000000000AA", ClaimToken: "tok-code",
		PaymentCodeHash: x402CodeHash("real-code"),
		Money:           1, Amount: 1, Status: model.X402HoldStatusHeld,
		CreatedAt: model.GetDBTimestamp(), ExpiresAt: model.GetDBTimestamp() + 900,
	}
	require.NoError(t, model.DB.Create(hold).Error)

	// 只带订单号 → 401,不查单不入账(防「猜到单号就领走别人挂账」)
	c, w := newPostContext(t, "/api/x402/invoke", map[string]string{"X-Out-Trade-No": hold.TradeNo})
	SkillInvoke(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "BAD_PAYMENT_CODE")

	// 带错码 → 同样 401
	c2, w2 := newPostContext(t, "/api/x402/invoke", map[string]string{
		"X-Out-Trade-No": hold.TradeNo, "WeixinPay-Required": "wrong-code"})
	SkillInvoke(c2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)

	// body 兜底路径也接受合法码
	c3, w3 := newPostContext(t, "/api/x402/invoke", map[string]string{"X-Out-Trade-No": hold.TradeNo})
	c3.Request.Body = io.NopCloser(strings.NewReader(`{"query":"ping","payment_code":"real-code"}`))
	SkillInvoke(c3)
	assert.NotEqual(t, http.StatusUnauthorized, w3.Code, "合法 payment_code 不应被判为未授权")

	var got model.X402Hold
	require.NoError(t, model.DB.First(&got, hold.Id).Error)
	assert.Equal(t, model.X402HoldStatusHeld, got.Status, "无证书时不得改动已入队状态")
	var topups int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&topups).Error)
	assert.Zero(t, topups)
}

func TestSkillInvokeRetryExpiredOrderCannotBeVerifiedWithoutCerts(t *testing.T) {
	setupX402ControllerTest(t)
	operation_setting.WechatMchID = ""
	operation_setting.WechatAppId = ""
	wechatNativeSvc = nil
	hold := &model.X402Hold{
		TradeNo: "WX402_EXPIRED0000000000000000A", ClaimToken: "tok-expired",
		PaymentCodeHash: x402CodeHash("code-expired"),
		Money:           1, Amount: 1, Status: model.X402HoldStatusPending,
		CreatedAt: model.GetDBTimestamp() - 1000, ExpiresAt: model.GetDBTimestamp() - 100,
	}
	require.NoError(t, model.DB.Create(hold).Error)

	c, w := newPostContext(t, "/api/x402/invoke", map[string]string{
		"X-Out-Trade-No": hold.TradeNo, "WeixinPay-Required": "code-expired"})
	SkillInvoke(c)

	// 过期不等于没扣款,所以不能直接回 PAYMENT_EXPIRED 了事 —— 必须查单。
	// 没证书就查不到单,只能 503,并且绝不入账、也不把 expired 洗成 held。
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "WECHAT_NOT_CONFIGURED")
	var got model.X402Hold
	require.NoError(t, model.DB.First(&got, hold.Id).Error)
	assert.Equal(t, model.X402HoldStatusExpired, got.Status)
	var topups int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&topups).Error)
	assert.Zero(t, topups)
}

func TestSkillInvokeRetryWithoutMerchantCertsCannotVerifyPayment(t *testing.T) {
	setupX402ControllerTest(t)
	operation_setting.WechatMchID = ""
	operation_setting.WechatAppId = ""
	wechatNativeSvc = nil
	hold := &model.X402Hold{
		TradeNo: "WX402_NOCERT000000000000000000A", ClaimToken: "tok-nocert",
		PaymentCodeHash: x402CodeHash("code-nocert"),
		Money:           1, Amount: 1, Status: model.X402HoldStatusPending,
		CreatedAt: model.GetDBTimestamp(), ExpiresAt: model.GetDBTimestamp() + 900,
	}
	require.NoError(t, model.DB.Create(hold).Error)

	c, w := newPostContext(t, "/api/x402/invoke", map[string]string{
		"X-Out-Trade-No": hold.TradeNo, "WeixinPay-Required": "code-nocert"})
	SkillInvoke(c)

	// 查不到单就绝不入账:503 而非 200,且状态仍为 pending
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "WECHAT_NOT_CONFIGURED")
	var got model.X402Hold
	require.NoError(t, model.DB.First(&got, hold.Id).Error)
	assert.Equal(t, model.X402HoldStatusPending, got.Status)
	var topups int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&topups).Error)
	assert.Zero(t, topups)
}

func TestSkillBindClaimRequiresSession(t *testing.T) {
	setupX402ControllerTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x402/claim?claim=abc", nil)
	SkillBindClaim(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSkillBindClaimUnknownToken(t *testing.T) {
	setupX402ControllerTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x402/claim?claim=nope", nil)
	SkillBindClaim(c)

	assert.Equal(t, http.StatusGone, w.Code)
}

func TestSkillWeChatClaimDisabledWithoutWeChatAuth(t *testing.T) {
	setupX402ControllerTest(t)
	original := common.WeChatAuthEnabled
	t.Cleanup(func() { common.WeChatAuthEnabled = original })
	common.WeChatAuthEnabled = false

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x402/wechat/claim?code=x&claim=y", nil)
	SkillWeChatClaim(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "未开启")
}

func TestSkillBindPageRendersHold(t *testing.T) {
	setupX402ControllerTest(t)
	hold := &model.X402Hold{
		TradeNo: "WX402_PAGE00000000000000000000AB", ClaimToken: "tok-page",
		Money: 3.5, Amount: 3, Status: model.X402HoldStatusHeld, PayTime: model.GetDBTimestamp(),
		CreatedAt: model.GetDBTimestamp(), ExpiresAt: model.GetDBTimestamp() + 600,
	}
	require.NoError(t, model.DB.Create(hold).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x402/bind?claim=tok-page", nil)
	SkillBindPage(c)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "3.5")
	assert.Contains(t, body, hold.TradeNo)
	// claim 由 data-claim 注入,页面脚本拼 /api/x402/claim 调用(避免内联 JS 注入)
	assert.Contains(t, body, `data-claim="tok-page"`)
	assert.Contains(t, body, "/api/x402/claim?claim=")
	// 未配服务号授权模板时不得出现半截授权链接
	assert.NotContains(t, body, "{redirect_uri}")
}

func TestSkillBindPageBuildsOAuthRedirect(t *testing.T) {
	setupX402ControllerTest(t)
	hold := &model.X402Hold{
		TradeNo: "WX402_PAGE00000000000000000000CD", ClaimToken: "tok-oauth",
		Money: 1, Amount: 1, Status: model.X402HoldStatusHeld,
		CreatedAt: model.GetDBTimestamp(), ExpiresAt: model.GetDBTimestamp() + 600,
	}
	require.NoError(t, model.DB.Create(hold).Error)
	operation_setting.X402MpOAuthURL = "https://open.weixin.qq.com/connect/oauth2/authorize?appid=WX&redirect_uri={redirect_uri}&scope=snsapi_base#wechat_redirect"

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x402/bind?claim=tok-oauth", nil)
	c.Request.Host = "scheng.net"
	SkillBindPage(c)

	body := w.Body.String()
	assert.Contains(t, body, "redirect_uri=")
	assert.Contains(t, body, "%2Fapi%2Fx402%2Fbind%3Fclaim%3Dtok-oauth")
	assert.NotContains(t, body, "{redirect_uri}")
}

func TestSkillBindPageRejectsBadClaim(t *testing.T) {
	setupX402ControllerTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x402/bind?claim=missing", nil)
	SkillBindPage(c)
	assert.Equal(t, http.StatusGone, w.Code)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/x402/bind", nil)
	SkillBindPage(c2)
	assert.Equal(t, http.StatusBadRequest, w2.Code)
}

func TestSkillHoldsStatusRequiresSession(t *testing.T) {
	setupX402ControllerTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x402/holds", nil)
	SkillHoldsStatus(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSkillHoldsStatusListsMine(t *testing.T) {
	setupX402ControllerTest(t)
	user := &model.User{Username: "x402ctl" + fmt.Sprint(common.GetTimestamp()), Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "affx402"}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.BindX402AgentId("agent-ctl", user.Id))

	hold := &model.X402Hold{
		TradeNo: "WX402_LIST00000000000000000000AB", ClaimToken: "tok-list", AgentUserId: "agent-ctl",
		Money: 2, Amount: 2, Status: model.X402HoldStatusHeld,
		CreatedAt: model.GetDBTimestamp(), ExpiresAt: model.GetDBTimestamp() + 600,
	}
	require.NoError(t, model.DB.Create(hold).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", user.Id)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x402/holds", nil)
	SkillHoldsStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), hold.TradeNo)
	assert.Contains(t, w.Body.String(), `"pending_count":1`)
}
