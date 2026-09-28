package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAgentJsapiTestEngine(t *testing.T, sessionOpenid string, userId int) *gin.Engine {
	t.Helper()
	r := gin.New()
	r.Use(sessions.Sessions("wechat_jsapi_session", cookie.NewStore([]byte("agent-jsapi-test-secret"))))
	r.Use(func(c *gin.Context) {
		if sessionOpenid != "" {
			s := sessions.Default(c)
			s.Set(wechatJsapiOpenidSessionKey, sessionOpenid)
			_ = s.Save()
		}
		if userId != 0 {
			c.Set("id", userId)
		}
		c.Next()
	})
	r.POST("/api/user/agent/wechat/jsapi/topup", CreateAgentJsapiTopUp)
	return r
}

func setupAgentJsapiTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

func postAgentJsapi(engine *gin.Engine, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/agent/wechat/jsapi/topup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	return w
}

// 未走静默授权(session 无 openid)→ wechat_oauth_required，前端据此跳授权后重试。
// 该契约对齐 RequestWechatJsapiPay，前端按它分支，改了就断。
func TestCreateAgentJsapiTopUpRequiresOauth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAgentJsapiTestDB(t)

	w := postAgentJsapi(newAgentJsapiTestEngine(t, "", 0), `{"amount_yuan":1}`)
	assert.Contains(t, w.Body.String(), "wechat_oauth_required")
}

// 金额非法在取 openid 之前就拒，不产生订单。
func TestCreateAgentJsapiTopUpRejectsBadAmount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAgentJsapiTestDB(t)

	w := postAgentJsapi(newAgentJsapiTestEngine(t, "o-mp-payer", 0), `{"amount_yuan":0}`)
	assert.Contains(t, w.Body.String(), "参数错误")
	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count, "参数被拒时不得建单")
}

// 钱必须进"付款那个微信号"所属的账户：openid 已绑 A、当前浏览器却登录着 B 时，
// 订单归 A。否则 A 用微信付的款会记到 B 的账户上。
// ponytail: 断言建单落库那一刻的 user_id——JSAPI 预下单在写库之后才发生且需真商户号，
// 故此处预下单必失败、接口回"下单失败"，但归属判定已经钉死在订单行里。
func TestCreateAgentJsapiTopUpPaysIntoOpenidAccountNotSessionUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAgentJsapiTestDB(t)
	setWechatConfigured()
	t.Cleanup(func() { wechatJsapiSvc = nil; clearWechatConfig() })

	// aff_code 有唯一约束，空串会撞，必须给不同值
	payer := &model.User{Id: 11, Username: "payer-a", Password: "x", AffCode: "aff-payer-a",
		Status: common.UserStatusEnabled, MpOpenid: "o-mp-payer"}
	bystander := &model.User{Id: 22, Username: "logged-in-b", Password: "x", AffCode: "aff-bystander-b",
		Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(payer).Error)
	require.NoError(t, model.DB.Create(bystander).Error)

	w := postAgentJsapi(newAgentJsapiTestEngine(t, "o-mp-payer", 22), `{"amount_yuan":1}`)
	assert.Contains(t, w.Body.String(), "下单失败") // 假商户号，预下单必失败

	var got model.TopUp
	require.NoError(t, model.DB.Where("user_id <> 0").First(&got).Error)
	assert.Equal(t, 11, got.UserId, "订单必须归付款微信号所属账户，而非当前登录态账户")
}
