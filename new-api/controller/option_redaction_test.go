package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedactionMasksPrivateKeyPEM 证明 GetOptions 对 WechatPrivateKeyPEM 走 redaction 门控,
// 返回明文即视为泄漏(此为 money-path 安全断言,不可懒掉)。
// 同时回归保护: AlipayAppPrivateKey / WechatAPIv3Key 等已掩字段仍掩码。
func TestRedactionMasksPrivateKeyPEM(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "BEGIN-FAKE-PRIVATE-KEY-BLOCK-DO-NOT-LEAK"
	sensitive := map[string]string{
		"WechatPrivateKeyPEM":  secret,
		"AlipayAppPrivateKey":  "alipay-priv-secret",
		"WechatAPIv3Key":       "apiv3-secret",
		"AlipayPublicKey":      "alipay-pub-value",
		"PaymentSetting.Token": "tok-secret",
	}
	safe := map[string]string{
		"WechatAppId": "wxabc",
		"SystemName":  "Savvy Agent",
		"RetryTimes":  "3",
	}

	// ponytail: GetOptions 内部自取 OptionMapRWMutex.Lock; 此处先写锁写入, 必须在调 GetOptions 前 Unlock, 否则死锁。
	common.OptionMapRWMutex.Lock()
	// ponytail: OptionMap 包级 nil map, 进程未跑过 InitOptionMap 时写入会 panic; 仅在 nil 时 make, 复原时也只清空本测写入的键(不把 nil 还回去以免干扰后续测试)。
	prevMap := common.OptionMap
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string, len(safe)+len(sensitive))
	}
	for k, v := range safe {
		common.OptionMap[k] = v
	}
	for k, v := range sensitive {
		common.OptionMap[k] = v
	}
	common.OptionMapRWMutex.Unlock()
	// ponytail: 复原 OptionMap 保测试间隔离 + fork-safe(共享进程可能其他测试已写入)。
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		for k := range safe {
			delete(common.OptionMap, k)
		}
		for k := range sensitive {
			delete(common.OptionMap, k)
		}
		// 若本测之前是 nil, 还原 nil; 否则保留(可能其他测试已写入)。
		if prevMap == nil {
			common.OptionMap = nil
		}
		common.OptionMapRWMutex.Unlock()
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/option", nil)
	GetOptions(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Success bool            `json:"success"`
		Data    []*model.Option `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)

	got := map[string]string{}
	for _, o := range resp.Data {
		got[o.Key] = o.Value
	}

	// 安全断言: 所有敏感字段必须被 redaction 掉(不在响应中)。
	for k, v := range sensitive {
		_, present := got[k]
		assert.Falsef(t, present, "敏感字段 %s 泄漏明文: 值=%q 应被 GetOptions redaction 门控掩掉", k, v)
	}
	// 回归断言: 安全字段照常返回。
	assert.Equal(t, "wxabc", got["WechatAppId"], "非敏感 WechatAppId 应原样返回")
	assert.Equal(t, "Savvy Agent", got["SystemName"], "非敏感 SystemName 应原样返回")
	assert.Equal(t, "3", got["RetryTimes"], "非敏感 RetryTimes 应原样返回")
}
