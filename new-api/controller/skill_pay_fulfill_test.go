package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSkillPayFulfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	// GetUserGroup 依赖包级列名变量(commonGroupCol,仅由 model.InitDB→initCol 填充)；
	// 对齐 model_list_test 约定：先经 InitDB 初始化列名，再换用本测试私有内存库。
	origDSN, hadDSN := os.LookupEnv("SQL_DSN")
	origTypes := [2]common.DatabaseType{common.MainDatabaseType(), common.LogDatabaseType()}
	origMaster := common.IsMasterNode
	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	prevDB, prevLog := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = prevDB, prevLog
		common.IsMasterNode = origMaster
		common.SetDatabaseTypes(origTypes[0], origTypes[1])
		if hadDSN {
			_ = os.Setenv("SQL_DSN", origDSN)
		} else {
			_ = os.Unsetenv("SQL_DSN")
		}
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.SkillPayOrder{}))
	return db
}

func newFulfillCtx(sessionUserId int) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/skill/invoke", nil)
	c.Request.RemoteAddr = "10.0.0.1:1234"
	if sessionUserId > 0 {
		c.Set("id", sessionUserId)
	}
	return c
}

func seedFulfillUser(t *testing.T, db *gorm.DB, id int, wechatId string) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{
		Id: id, Username: fmt.Sprintf("sp-u%d", id), Group: "default",
		Status: common.UserStatusEnabled, WeChatId: wechatId,
		AffCode: fmt.Sprintf("aff_sp_%d", id),
	}).Error)
}

// X402 topup 履约的三级身份判定：登录态 > 付款人 openid 匹配 > 游客认领。
// 第二级是"老客户零点击直入账"的核心；openid 空间与扫码登录(users.wechat_id)同源。
func TestFulfillSkillPayTopUpIdentityTiers(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	prevPrice, prevServer := operation_setting.Price, system_setting.ServerAddress
	operation_setting.Price = 1.0 // 1 元 = 1 额度单位，断言直观
	system_setting.ServerAddress = "https://savvy.test"
	t.Cleanup(func() {
		operation_setting.Price, system_setting.ServerAddress = prevPrice, prevServer
	})

	seedFulfillUser(t, db, 101, "")
	seedFulfillUser(t, db, 102, "openid-old-customer")
	seedFulfillUser(t, db, 103, "openid-disabled")
	require.NoError(t, db.Model(&model.User{Id: 103}).Update("status", common.UserStatusDisabled).Error)

	t.Run("登录态优先直接入账", func(t *testing.T) {
		content, err := fulfillSkillPayTopUp(newFulfillCtx(101), "WX402_T1", "tx1", 10, "openid-old-customer")
		require.NoError(t, err)
		assert.Contains(t, content, "充值成功")
		var tu model.TopUp
		require.NoError(t, db.Where("trade_no = ?", "WX402_T1").First(&tu).Error)
		assert.Equal(t, 101, tu.UserId) // 即使 payer openid 指向 102，登录态赢
		var u model.User
		require.NoError(t, db.First(&u, 101).Error)
		assert.Positive(t, u.Quota)
	})

	t.Run("付款人openid命中老客户零点击入账", func(t *testing.T) {
		content, err := fulfillSkillPayTopUp(newFulfillCtx(0), "WX402_T2", "tx2", 10, "openid-old-customer")
		require.NoError(t, err)
		assert.Contains(t, content, "充值成功")
		assert.NotContains(t, content, "claim_token")
		var tu model.TopUp
		require.NoError(t, db.Where("trade_no = ?", "WX402_T2").First(&tu).Error)
		assert.Equal(t, 102, tu.UserId)
	})

	t.Run("openid无主走认领不发钱", func(t *testing.T) {
		content, err := fulfillSkillPayTopUp(newFulfillCtx(0), "WX402_T3", "tx3", 10, "openid-stranger")
		require.NoError(t, err)
		assert.Contains(t, content, "claim_token")
		assert.Contains(t, content, "https://savvy.test/agent?claim_token=")
		var tu model.TopUp
		require.NoError(t, db.Where("trade_no = ?", "WX402_T3").First(&tu).Error)
		assert.Zero(t, tu.UserId)
		assert.Len(t, tu.ClaimToken, 32)
	})

	t.Run("禁用账号openid不静默入账", func(t *testing.T) {
		content, err := fulfillSkillPayTopUp(newFulfillCtx(0), "WX402_T4", "tx4", 10, "openid-disabled")
		require.NoError(t, err)
		assert.Contains(t, content, "claim_token")
		var tu model.TopUp
		require.NoError(t, db.Where("trade_no = ?", "WX402_T4").First(&tu).Error)
		assert.Zero(t, tu.UserId)
	})

	t.Run("金额过小换算为零降级认领", func(t *testing.T) {
		content, err := fulfillSkillPayTopUp(newFulfillCtx(0), "WX402_T5", "tx5", 0.01, "openid-old-customer")
		require.NoError(t, err) // 不硬失败：失败会让 AI 无限重试
		assert.Contains(t, content, "claim_token")
		var tu model.TopUp
		require.NoError(t, db.Where("trade_no = ?", "WX402_T5").First(&tu).Error)
		assert.Zero(t, tu.UserId)
		assert.Equal(t, common.TopUpStatusSuccess, tu.Status) // 钱不丢，等认领
	})
}

// 付款人身份回写：Mark 条件更新 + Bind 仅空值补记，notify/查单先后到达都不覆盖。
func TestSkillPayPayerOpenidPersistence(t *testing.T) {
	db := setupSkillPayFulfillTestDB(t)
	require.NoError(t, db.Create(&model.SkillPayOrder{
		OutTradeNo: "WX402_P1", Kind: model.SkillPayKindTopUp, Status: model.SkillPayStatusPending,
	}).Error)

	require.NoError(t, model.MarkSkillPayOrderPaid("WX402_P1", "tx9", ""))
	var o model.SkillPayOrder
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_P1").First(&o).Error)
	assert.Equal(t, model.SkillPayStatusPaid, o.Status)
	assert.Empty(t, o.PayerOpenid)

	require.NoError(t, model.BindSkillPayPayerOpenid("WX402_P1", "openid-a"))
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_P1").First(&o).Error)
	assert.Equal(t, "openid-a", o.PayerOpenid)

	// 已 paid 后 Mark 不再改状态，但空值补记仍生效；已有值不被覆盖
	require.NoError(t, model.MarkSkillPayOrderPaid("WX402_P1", "tx10", "openid-b"))
	require.NoError(t, model.BindSkillPayPayerOpenid("WX402_P1", "openid-c"))
	require.NoError(t, db.Where("out_trade_no = ?", "WX402_P1").First(&o).Error)
	assert.Equal(t, "openid-a", o.PayerOpenid)
	assert.Equal(t, "tx9", o.TransactionId)
}
