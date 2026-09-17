package controller

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlipayAuditFromForm(t *testing.T) {
	form := url.Values{}
	form.Set("trade_no", "2026091722001400001234567890")
	form.Set("buyer_id", "2088102177823456")
	form.Set("buyer_logon_id", "138****1234")
	form.Set("gmt_payment", "2026-09-17 14:30:40")
	audit, err := alipayAuditFromForm(form)
	require.NoError(t, err)
	assert.Equal(t, "2026091722001400001234567890", audit.ChannelTradeNo)
	assert.Equal(t, "2088102177823456", audit.PayerId)
	assert.Equal(t, "138****1234", audit.PayerAccount)
	// 控制器裁定: 期望值动态计算,不硬编码 Unix 秒(时区相关)
	want := time.Date(2026, 9, 17, 14, 30, 40, 0, time.Local).Unix()
	assert.Equal(t, want, audit.ChannelPayTime)
}

func TestAlipayAuditFromForm_EmptyGmtPayment(t *testing.T) {
	form := url.Values{}
	form.Set("trade_no", "T1")
	audit, err := alipayAuditFromForm(form)
	require.NoError(t, err, "缺 gmt_payment 不报错")
	assert.Zero(t, audit.ChannelPayTime)
}
