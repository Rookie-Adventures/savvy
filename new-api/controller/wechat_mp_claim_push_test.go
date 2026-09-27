package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMpTopUpReply 锁住服务号充值回推的三档话术:绑定直入账 / 游客给认领链接 / 中间态不推。
func TestMpTopUpReply(t *testing.T) {
	prev := system_setting.ServerAddress
	system_setting.ServerAddress = "https://scheng.net/"
	t.Cleanup(func() { system_setting.ServerAddress = prev })

	cases := []struct {
		name  string
		tu    *model.TopUp
		check func(*testing.T, string)
	}{
		{"未成功不推", &model.TopUp{Status: common.TopUpStatusPending, UserId: 7}, func(t *testing.T, s string) {
			assert.Empty(t, s)
		}},
		{"绑定用户报到账", &model.TopUp{Status: common.TopUpStatusSuccess, UserId: 7, Money: 1.5}, func(t *testing.T, s string) {
			assert.Contains(t, s, "已到账")
			assert.Contains(t, s, "1.50")
			assert.NotContains(t, s, "claim_token")
		}},
		{"游客给认领链接", &model.TopUp{Status: common.TopUpStatusSuccess, UserId: 0, Money: 0.1, ClaimToken: strings.Repeat("a", 32)}, func(t *testing.T, s string) {
			assert.Contains(t, s, "/agent?claim_token="+strings.Repeat("a", 32))
			assert.True(t, strings.HasPrefix(s, "✅"))
		}},
		{"无凭据不推", &model.TopUp{Status: common.TopUpStatusSuccess, UserId: 0, Money: 0.1}, func(t *testing.T, s string) {
			assert.Empty(t, s)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotNil(t, tc.tu)
			tc.check(t, mpTopUpReply(tc.tu))
		})
	}
}
