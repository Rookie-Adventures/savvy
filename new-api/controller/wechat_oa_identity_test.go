package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// controller 包测试基建:包内其他测试(token_test/model_list_test)会按测试替换全局
// model.DB 并在 cleanup 关闭 —— 本特性测试同样每测试自带 DB(shared-cache 内存库),
// 不依赖也不污染其他测试的 DB 状态。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func newWechatOATestEngine() *gin.Engine {
	r := gin.New()
	store := cookie.NewStore([]byte("wechat-oa-test-secret"))
	r.Use(sessions.Sessions("wechat_oa_session", store))
	r.POST("/api/wechat/oa/tokens", CreateWeChatOAIdentityToken)
	r.GET("/api/wechat/oa/tokens/:token/status", GetWeChatOATokenStatus)
	r.GET("/api/wechat/oa/entry", WeChatOAEntry)
	r.GET("/api/wechat/oa/callback", WeChatOACallback)
	r.POST("/api/user/wechat/oa/tokens", CreateWeChatOABindToken)
	r.POST("/api/wechat/oa/login/claim", ClaimWeChatOALogin)
	r.POST("/api/user/wechat/oa/bind/claim-existing", ClaimExistingWeChatOABind)
	r.GET("/api/user/wechat/oa/binding", GetWeChatOABinding)
	r.DELETE("/api/user/wechat/oa/binding", DeleteWeChatOABinding)
	// 测试辅助:预置登录态 session(模拟已登录用户携带 cookie;id 须为 int,对齐 setupLogin)
	r.GET("/test/session/:id", func(c *gin.Context) {
		n, _ := strconv.Atoi(c.Param("id"))
		s := sessions.Default(c)
		s.Set("id", n)
		_ = s.Save()
		c.String(http.StatusOK, "ok")
	})
	return r
}

func withStubExchange(t *testing.T, openid string, retErr error) {
	t.Helper()
	orig := wechatExchangeCodeFn
	wechatExchangeCodeFn = func(ctx context.Context, code string) (string, error) {
		return openid, retErr
	}
	t.Cleanup(func() { wechatExchangeCodeFn = orig })
}

// setupWeChatOATestDB 每测试独立的 shared-cache 内存库(对齐 openTokenControllerTestDB 范式),
// 迁移本特性所需表。
func setupWeChatOATestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db
	if err := db.AutoMigrate(&model.User{}, &model.Log{}, &model.WeChatAccount{}, &model.WeChatOAuthToken{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

// setWechatOAConfig 配置服务号参数 + 每测试 DB。
func setWechatOAConfig(t *testing.T) {
	t.Helper()
	setupWeChatOATestDB(t)
	operation_setting.WechatMpAppId = "wx-mp-test"
	operation_setting.WechatAppSecret = "secret-test"
	t.Cleanup(func() {
		operation_setting.WechatMpAppId = ""
		operation_setting.WechatAppSecret = ""
	})
}

func expireToken(t *testing.T, token string) {
	t.Helper()
	model.DB.Model(&model.WeChatOAuthToken{}).Where("token = ?", token).
		Update("expires_at", time.Now().Add(-time.Minute).Unix())
}

// ---- entry ----

func TestWeChatOAEntryExpiredTokenRejected(t *testing.T) {
	setWechatOAConfig(t)
	tok, err := model.CreateWeChatOAuthToken("login", 0)
	if err != nil {
		t.Fatal(err)
	}
	expireToken(t, tok.Token)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/entry?state=login:"+tok.Token, nil)
	newWechatOATestEngine().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expired entry should 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestWeChatOAEntryValidRedirectsToAuthorize(t *testing.T) {
	setWechatOAConfig(t)
	tok, err := model.CreateWeChatOAuthToken("direct", 0)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/entry?state=direct:"+tok.Token, nil)
	newWechatOATestEngine().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("valid entry should 302, got %d %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "open.weixin.qq.com") || !strings.Contains(loc, "state=direct%3A"+tok.Token) {
		t.Fatalf("unexpected authorize url: %s", loc)
	}
	if !strings.Contains(loc, "redirect_uri=") {
		t.Fatalf("authorize url missing redirect_uri: %s", loc)
	}
}

// ---- callback: bind ----

func TestWeChatOACallbackBindOpenidTakenRejected(t *testing.T) {
	setWechatOAConfig(t)
	withStubExchange(t, "o-taken", nil)
	if err := model.CreateWeChatAccount(&model.WeChatAccount{
		UserId: 2, Provider: "oa", AppId: "wx-mp-test", Openid: "o-taken",
	}); err != nil {
		t.Fatal(err)
	}
	tok, err := model.CreateWeChatOAuthToken("bind", 1)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/callback?code=abc&state=bind:"+tok.Token, nil)
	newWechatOATestEngine().ServeHTTP(w, req)

	got, err := model.GetWeChatOAuthTokenByToken(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "rejected" {
		t.Fatalf("openid taken should consume as rejected, got %s (%s)", got.Status, w.Body.String())
	}
	// 绝不覆盖:原绑定仍在
	if _, err := model.GetWeChatAccountByUserId(2, "oa", "wx-mp-test"); err != nil {
		t.Fatalf("existing binding must survive: %v", err)
	}
}

func TestWeChatOACallbackBindSuccess(t *testing.T) {
	setWechatOAConfig(t)
	withStubExchange(t, "o-bind-new", nil)
	tok, err := model.CreateWeChatOAuthToken("bind", 3)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/callback?code=abc&state=bind:"+tok.Token, nil)
	newWechatOATestEngine().ServeHTTP(w, req)

	got, err := model.GetWeChatOAuthTokenByToken(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" {
		t.Fatalf("bind should complete, got %s (%s)", got.Status, w.Body.String())
	}
	acc, err := model.GetWeChatAccountByOpenid("oa", "wx-mp-test", "o-bind-new")
	if err != nil || acc.UserId != 3 {
		t.Fatalf("binding not created: %v %+v", err, acc)
	}
}

// ---- callback: login 未绑 → authorized ----

func TestWeChatOACallbackLoginUnboundAuthorized(t *testing.T) {
	setWechatOAConfig(t)
	withStubExchange(t, "o-login-new", nil)
	tok, err := model.CreateWeChatOAuthToken("login", 0)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/callback?code=abc&state=login:"+tok.Token, nil)
	newWechatOATestEngine().ServeHTTP(w, req)

	got, err := model.GetWeChatOAuthTokenByToken(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "authorized" || got.OpenidPending != "o-login-new" {
		t.Fatalf("unbound login should be authorized with openid pending, got %s/%s (%s)",
			got.Status, got.OpenidPending, w.Body.String())
	}
}

// ---- callback: direct 已绑 → 会话落地 + 302 /console/topup ----

func TestWeChatOACallbackDirectBoundLogsIn(t *testing.T) {
	setWechatOAConfig(t)
	withStubExchange(t, "o-direct-bound", nil)
	user := &model.User{Username: fmt.Sprintf("wx_direct_%d", time.Now().UnixNano()), Password: "x", DisplayName: "微信用户", Role: 1, Status: 1}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateWeChatAccount(&model.WeChatAccount{
		UserId: user.Id, Provider: "oa", AppId: "wx-mp-test", Openid: "o-direct-bound",
	}); err != nil {
		t.Fatal(err)
	}
	tok, err := model.CreateWeChatOAuthToken("direct", 0)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/callback?code=abc&state=direct:"+tok.Token, nil)
	newWechatOATestEngine().ServeHTTP(w, req)

	if w.Code != http.StatusFound || w.Header().Get("Location") != "/console/topup" {
		t.Fatalf("direct bound should 302 /console/topup, got %d %s", w.Code, w.Header().Get("Location"))
	}
	got, err := model.GetWeChatOAuthTokenByToken(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "consumed" || got.UserId != user.Id {
		t.Fatalf("direct bound should consume with bound user, got %s/%d", got.Status, got.UserId)
	}
	// 会话必须已落地(session id == user.Id)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after direct login")
	}
}

// ---- callback: direct 未绑 → 302 /sign-in?wx_token= ----

func TestWeChatOACallbackDirectUnboundRedirectsSignIn(t *testing.T) {
	setWechatOAConfig(t)
	withStubExchange(t, "o-direct-new", nil)
	tok, err := model.CreateWeChatOAuthToken("direct", 0)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/callback?code=abc&state=direct:"+tok.Token, nil)
	newWechatOATestEngine().ServeHTTP(w, req)
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/sign-in?wx_token="+tok.Token) {
		t.Fatalf("direct unbound should 302 sign-in with wx_token, got %d %s", w.Code, w.Header().Get("Location"))
	}
}

// ---- tokens: login/direct 匿名创建 ----

func TestWeChatOACreateIdentityToken(t *testing.T) {
	setWechatOAConfig(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/wechat/oa/tokens", strings.NewReader(`{"kind":"login"}`))
	newWechatOATestEngine().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("create login token failed: %d %s", w.Code, w.Body.String())
	}
	// 非法 kind 必须拒
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/wechat/oa/tokens", strings.NewReader(`{"kind":"bind"}`))
	newWechatOATestEngine().ServeHTTP(w2, req2)
	if w2.Code == http.StatusOK {
		t.Fatalf("anonymous bind kind must be rejected: %s", w2.Body.String())
	}
}

// ---- tokens: bind 需登录态 ----

func TestWeChatOACreateBindTokenRequiresLogin(t *testing.T) {
	setWechatOAConfig(t)
	r := newWechatOATestEngine()

	// 无登录态 → 拒
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/wechat/oa/tokens", strings.NewReader(`{"kind":"bind"}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bind token without session should 401, got %d", w.Code)
	}

	// 有登录态 → token 归属当前用户,qr_url 指向 entry
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/test/session/42", nil))
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/user/wechat/oa/tokens", strings.NewReader(`{"kind":"bind"}`))
	for _, ck := range w2.Result().Cookies() {
		req3.AddCookie(ck)
	}
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("bind token with session should 200, got %d %s", w3.Code, w3.Body.String())
	}
	if !strings.Contains(w3.Body.String(), "qr_url") {
		t.Fatalf("bind token response missing qr_url: %s", w3.Body.String())
	}
}

// ---- Task 3: claim 三路径 + 解绑 ----

// makeTokenAuthorized 直改 DB,把票证置为 authorized/callback 产物(免走真实微信授权)。
func makeTokenAuthorized(t *testing.T, kind string, userId int, openid string) *model.WeChatOAuthToken {
	t.Helper()
	tok, err := model.CreateWeChatOAuthToken(kind, userId)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Model(&model.WeChatOAuthToken{}).Where("token = ?", tok.Token).
		Updates(map[string]interface{}{"status": "authorized", "openid_pending": openid}).Error; err != nil {
		t.Fatal(err)
	}
	return tok
}

func seedUser(t *testing.T, username string) *model.User {
	t.Helper()
	u := &model.User{Username: username, Password: "x", DisplayName: "微信用户", Role: 1, Status: 1, AffCode: username}
	if err := model.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func testSessionCookies(t *testing.T, r http.Handler, id int) []*http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/test/session/%d", id), nil)
	r.ServeHTTP(w, req)
	return w.Result().Cookies()
}

func TestWeChatOAClaimCreateCreatesUserAndConsumes(t *testing.T) {
	setWechatOAConfig(t)
	common.RegisterEnabled = true
	t.Cleanup(func() { common.RegisterEnabled = false })
	r := newWechatOATestEngine()

	tok := makeTokenAuthorized(t, "login", 0, "o-claim-new")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/wechat/oa/login/claim",
		strings.NewReader(fmt.Sprintf(`{"token":%q,"mode":"create"}`, tok.Token)))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("claim create failed: %d %s", w.Code, w.Body.String())
	}

	// token 已消费;二次使用必须拒
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/wechat/oa/login/claim",
		strings.NewReader(fmt.Sprintf(`{"token":%q,"mode":"create"}`, tok.Token)))
	r.ServeHTTP(w2, req2)
	if w2.Code == http.StatusOK {
		t.Fatalf("second claim must fail: %s", w2.Body.String())
	}

	// 绑定落地 + 用户创建 + 初始密码可哈希验证
	acc, err := model.GetWeChatAccountByOpenid("oa", "wx-mp-test", "o-claim-new")
	if err != nil {
		t.Fatalf("binding not created: %v", err)
	}
	u, err := model.GetUserById(acc.UserId, false)
	if err != nil {
		t.Fatalf("created user missing: %v", err)
	}
	if u.DisplayName != "微信用户" {
		t.Fatalf("unexpected display name: %s", u.DisplayName)
	}
	if !strings.Contains(w.Body.String(), "initial_password") {
		t.Fatalf("response missing initial_password: %s", w.Body.String())
	}
}

func TestWeChatOAClaimLoginConsumes(t *testing.T) {
	setWechatOAConfig(t)
	r := newWechatOATestEngine()
	u := seedUser(t, fmt.Sprintf("wx_login_claim_%d", time.Now().UnixNano()))

	// login 流程:callback 已把 completed+UserId 落票证;此处直造该状态
	tok, _ := model.CreateWeChatOAuthToken("login", 0)
	model.DB.Model(&model.WeChatOAuthToken{}).Where("token = ?", tok.Token).
		Updates(map[string]interface{}{"status": "completed", "user_id": u.Id})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/wechat/oa/login/claim",
		strings.NewReader(fmt.Sprintf(`{"token":%q,"mode":"login"}`, tok.Token)))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("claim login failed: %d %s", w.Code, w.Body.String())
	}
	got, _ := model.GetWeChatOAuthTokenByToken(tok.Token)
	if got.Status != "consumed" {
		t.Fatalf("claim login should consume, got %s", got.Status)
	}

	// 二次使用必须拒(单用性)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/wechat/oa/login/claim",
		strings.NewReader(fmt.Sprintf(`{"token":%q,"mode":"login"}`, tok.Token)))
	r.ServeHTTP(w2, req2)
	if w2.Code == http.StatusOK {
		t.Fatalf("second claim login must fail: %s", w2.Body.String())
	}
}

func TestWeChatOAClaimLoginRejectsWrongStatus(t *testing.T) {
	setWechatOAConfig(t)
	r := newWechatOATestEngine()
	// authorized 票证不能走 mode=login(completed 才行)
	tok := makeTokenAuthorized(t, "login", 0, "o-wrong-status")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/wechat/oa/login/claim",
		strings.NewReader(fmt.Sprintf(`{"token":%q,"mode":"login"}`, tok.Token)))
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("claim login on authorized token must fail: %s", w.Body.String())
	}
}

func TestWeChatOABindClaimExisting(t *testing.T) {
	setWechatOAConfig(t)
	r := newWechatOATestEngine()
	owner := seedUser(t, fmt.Sprintf("wx_bind_owner_%d", time.Now().UnixNano()))
	claimer := seedUser(t, fmt.Sprintf("wx_claimer_%d", time.Now().UnixNano()))

	// 已占用 openid → 拒,绑定不变
	if err := model.CreateWeChatAccount(&model.WeChatAccount{
		UserId: owner.Id, Provider: "oa", AppId: "wx-mp-test", Openid: "o-occupied",
	}); err != nil {
		t.Fatal(err)
	}
	tok := makeTokenAuthorized(t, "login", 0, "o-occupied")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/wechat/oa/bind/claim-existing",
		strings.NewReader(fmt.Sprintf(`{"token":%q}`, tok.Token)))
	for _, ck := range testSessionCookies(t, r, claimer.Id) {
		req.AddCookie(ck)
	}
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("claim-existing on occupied openid must fail: %s", w.Body.String())
	}
	if _, err := model.GetWeChatAccountByUserId(owner.Id, "oa", "wx-mp-test"); err != nil {
		t.Fatalf("owner binding must survive: %v", err)
	}

	// 空闲 openid → 绑到 session 用户 + consumed
	tok2 := makeTokenAuthorized(t, "login", 0, "o-free")
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/user/wechat/oa/bind/claim-existing",
		strings.NewReader(fmt.Sprintf(`{"token":%q}`, tok2.Token)))
	for _, ck := range testSessionCookies(t, r, claimer.Id) {
		req2.AddCookie(ck)
	}
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("claim-existing free openid failed: %d %s", w2.Code, w2.Body.String())
	}
	acc, err := model.GetWeChatAccountByOpenid("oa", "wx-mp-test", "o-free")
	if err != nil || acc.UserId != claimer.Id {
		t.Fatalf("binding should belong to claimer: %v %+v", err, acc)
	}
	got, _ := model.GetWeChatOAuthTokenByToken(tok2.Token)
	if got.Status != "consumed" {
		t.Fatalf("claim-existing should consume, got %s", got.Status)
	}
}

func TestWeChatOABindingGetDelete(t *testing.T) {
	setWechatOAConfig(t)
	r := newWechatOATestEngine()
	u := seedUser(t, fmt.Sprintf("wx_binding_%d", time.Now().UnixNano()))
	cookies := testSessionCookies(t, r, u.Id)
	withCookies := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/user/wechat/oa/binding", nil)
		for _, ck := range cookies {
			req.AddCookie(ck)
		}
		return req
	}

	// 未绑定
	w := httptest.NewRecorder()
	r.ServeHTTP(w, withCookies())
	if !strings.Contains(w.Body.String(), `"bound":false`) {
		t.Fatalf("unbound expected, got %s", w.Body.String())
	}

	// 绑定后:bound + masked openid
	longOpenid := "o-111222333444"
	if err := model.CreateWeChatAccount(&model.WeChatAccount{
		UserId: u.Id, Provider: "oa", AppId: "wx-mp-test", Openid: longOpenid,
	}); err != nil {
		t.Fatal(err)
	}
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, withCookies())
	if !strings.Contains(w2.Body.String(), `"bound":true`) {
		t.Fatalf("bound expected, got %s", w2.Body.String())
	}
	masked := "o-1112****3444" // 前 6 后 4 中间打码
	if !strings.Contains(w2.Body.String(), masked) {
		t.Fatalf("masked openid expected in %s", w2.Body.String())
	}

	// DELETE 解绑后再查
	w3 := httptest.NewRecorder()
	delReq := httptest.NewRequest(http.MethodDelete, "/api/user/wechat/oa/binding", nil)
	for _, ck := range cookies {
		delReq.AddCookie(ck)
	}
	r.ServeHTTP(w3, delReq)
	if w3.Code != http.StatusOK {
		t.Fatalf("unbind failed: %d %s", w3.Code, w3.Body.String())
	}
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, withCookies())
	if !strings.Contains(w4.Body.String(), `"bound":false`) {
		t.Fatalf("unbound after delete expected, got %s", w4.Body.String())
	}
}

// ---- status: 票证即权限;bind 归属校验 ----

func TestWeChatOATokenStatus(t *testing.T) {
	setWechatOAConfig(t)
	r := newWechatOATestEngine()

	// login kind 匿名可查
	tok, _ := model.CreateWeChatOAuthToken("login", 0)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/wechat/oa/tokens/"+tok.Token+"/status", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"pending"`) {
		t.Fatalf("login status poll failed: %d %s", w.Code, w.Body.String())
	}

	// bind kind: 非归属者 403
	bindTok, _ := model.CreateWeChatOAuthToken("bind", 42)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/wechat/oa/tokens/"+bindTok.Token+"/status", nil))
	if w2.Code != http.StatusForbidden {
		t.Fatalf("bind status without owner session should 403, got %d", w2.Code)
	}

	// 归属者可查
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/test/session/42", nil))
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/api/wechat/oa/tokens/"+bindTok.Token+"/status", nil)
	for _, ck := range w3.Result().Cookies() {
		req4.AddCookie(ck)
	}
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("bind status with owner session should 200, got %d %s", w4.Code, w4.Body.String())
	}
}
