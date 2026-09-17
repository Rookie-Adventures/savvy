package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWxTopUpNotifyDetail(t *testing.T) {
	payload := `{"out_trade_no":"WXUSR1NOX1","transaction_id":"4200002376202609173123456789","trade_state":"SUCCESS","success_time":"2026-09-17T14:30:40+08:00","payer":{"openid":"oX-8k5abc"}}`
	detail, err := parseWxTopUpNotifyDetail(payload)
	require.NoError(t, err)
	assert.Equal(t, "4200002376202609173123456789", detail.TransactionId)
	assert.Equal(t, "oX-8k5abc", detail.Payer.Openid)
	assert.Equal(t, int64(1789626640), detail.SuccessTimeUnix) // brief 原值 1758090640 对应 2025-09-17,系笔误;2026-09-17T14:30:40+08:00 = 1789626640
}

func TestParseWxTopUpNotifyDetail_MissingFields(t *testing.T) {
	detail, err := parseWxTopUpNotifyDetail(`{"out_trade_no":"X"}`)
	require.NoError(t, err, "缺字段不报错,只是空值/0")
	assert.Empty(t, detail.TransactionId)
	assert.Zero(t, detail.SuccessTimeUnix)
}
