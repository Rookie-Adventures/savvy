package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 微信身份表测试:仅本文件需要这两张表,测试内自行 AutoMigrate(不动 TestMain 清单)。
func migrateWeChatIdentityTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&WeChatAccount{}, &WeChatOAuthToken{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM wechat_accounts")
		DB.Exec("DELETE FROM wechat_oauth_tokens")
	})
}

// 单用性:状态机单向推进,终态(consumed/rejected)不可再消费;过期 token Consume 必须失败。
// (completed→consumed 是 claim 的合法迁移,故"第二次 Consume"是否合法取决于目标终态——
// 真正的单用性保证是:每个 ticket 只能被消费到终态一次,之后任何 Consume 都失败。)
func TestWeChatOAuthTokenSingleUse(t *testing.T) {
	migrateWeChatIdentityTables(t)

	tok, err := CreateWeChatOAuthToken("login", 0)
	require.NoError(t, err)
	require.NotEmpty(t, tok.Token)
	require.Equal(t, "pending", tok.Status)
	// 128bit+ 票证:32 字节 hex = 64 字符
	require.Len(t, tok.Token, 64)
	require.True(t, tok.ExpiresAt > time.Now().Unix()+4*60, "ExpiresAt 应为 now+5min 量级")

	// callback: pending → completed(login 已绑)
	require.NoError(t, ConsumeWeChatOAuthToken(tok.Token, "completed", 7, ""))
	got, err := GetWeChatOAuthTokenByToken(tok.Token)
	require.NoError(t, err)
	require.Equal(t, "completed", got.Status)
	require.Equal(t, 7, got.UserId)
	require.True(t, got.CompletedAt > 0)

	// claim: completed → consumed(合法迁移,仅此一次)
	require.NoError(t, ConsumeWeChatOAuthToken(tok.Token, "consumed", 0, ""))

	// 终态后任何 Consume 必须失败
	err = ConsumeWeChatOAuthToken(tok.Token, "consumed", 0, "")
	require.Error(t, err, "consumed 是终态,再消费必须失败")

	// rejected 同为终态
	rej, err := CreateWeChatOAuthToken("direct", 0)
	require.NoError(t, err)
	require.NoError(t, ConsumeWeChatOAuthToken(rej.Token, "rejected", 0, "o-x"))
	err = ConsumeWeChatOAuthToken(rej.Token, "consumed", 1, "")
	require.Error(t, err, "rejected 是终态,再消费必须失败")

	// bind kind: userId 在创建时固化,消费时传 0 不得清掉归属
	bind, err := CreateWeChatOAuthToken("bind", 42)
	require.NoError(t, err)
	require.NoError(t, ConsumeWeChatOAuthToken(bind.Token, "completed", 0, ""))
	gotBind, err := GetWeChatOAuthTokenByToken(bind.Token)
	require.NoError(t, err)
	require.Equal(t, 42, gotBind.UserId, "消费时 userId=0 不得覆盖 bind 票证归属")

	// 过期 token Consume 必须失败
	exp := &WeChatOAuthToken{
		Token:     "expired-token-0001",
		Kind:      "login",
		Status:    "pending",
		CreatedAt: time.Now().Add(-10 * time.Minute).Unix(),
		ExpiresAt: time.Now().Add(-5 * time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(exp).Error)
	require.True(t, exp.IsExpired())
	err = ConsumeWeChatOAuthToken("expired-token-0001", "completed", 1, "")
	require.Error(t, err, "过期 token Consume 必须失败")
}

// openid 占用创建必须失败;同用户同 provider+app 二次绑定必须失败。
func TestWeChatAccountOpenidTaken(t *testing.T) {
	migrateWeChatIdentityTables(t)

	first := &WeChatAccount{UserId: 1, Provider: "oa", AppId: "wx-app", Openid: "o-111"}
	require.NoError(t, CreateWeChatAccount(first))

	// 同 (provider,app_id,openid) 二次创建必须 err,绝不覆盖
	dup := &WeChatAccount{UserId: 2, Provider: "oa", AppId: "wx-app", Openid: "o-111"}
	require.Error(t, CreateWeChatAccount(dup), "openid 已占用,创建必须失败")

	// 同 (user_id,provider,app_id) 二次绑定必须 err
	dupUser := &WeChatAccount{UserId: 1, Provider: "oa", AppId: "wx-app", Openid: "o-222"}
	require.Error(t, CreateWeChatAccount(dupUser), "用户已绑定同 provider+app,二次绑定必须失败")

	// 不同 app 的同 openid 允许(双 AppID 隔离范式)
	otherApp := &WeChatAccount{UserId: 2, Provider: "oa", AppId: "wx-other", Openid: "o-111"}
	require.NoError(t, CreateWeChatAccount(otherApp))

	// 查询
	got, err := GetWeChatAccountByOpenid("oa", "wx-app", "o-111")
	require.NoError(t, err)
	require.Equal(t, 1, got.UserId)
	_, err = GetWeChatAccountByOpenid("oa", "wx-app", "o-missing")
	require.Error(t, err)

	byUser, err := GetWeChatAccountByUserId(1, "oa", "wx-app")
	require.NoError(t, err)
	require.Equal(t, "o-111", byUser.Openid)

	// 解绑后可重新绑定
	require.NoError(t, DeleteWeChatAccountById(first.Id))
	_, err = GetWeChatAccountByOpenid("oa", "wx-app", "o-111")
	require.Error(t, err)
	rebind := &WeChatAccount{UserId: 9, Provider: "oa", AppId: "wx-app", Openid: "o-111"}
	require.NoError(t, CreateWeChatAccount(rebind))
}
