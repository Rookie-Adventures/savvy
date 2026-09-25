package controller

import (
	"math"
	"testing"
)

// 金额校验抽成 agentTopUpAmountCents 后必须钉死边界:
// 0.1 元测试单要放行、小于 1 分要拦(否则 round 成 0 分等于免单)、float 脏值不能 panic 成负数分。
func TestAgentTopUpAmountCents(t *testing.T) {
	cases := []struct {
		name     string
		yuan     float64
		wantCent int64
		wantOK   bool
	}{
		{name: "0.1 元测试单放行", yuan: 0.1, wantCent: 10, wantOK: true},
		{name: "最小 1 分", yuan: 0.01, wantCent: 1, wantOK: true},
		{name: "0.001 元不足 1 分被拦", yuan: 0.001, wantCent: 0, wantOK: false},
		{name: "零元被拦", yuan: 0, wantCent: 0, wantOK: false},
		{name: "负数被拦", yuan: -5, wantCent: 0, wantOK: false},
		{name: "NaN 被拦", yuan: math.NaN(), wantCent: 0, wantOK: false},
		{name: "正无穷被拦", yuan: math.Inf(1), wantCent: 0, wantOK: false},
		{name: "超 5000 上限被拦", yuan: 5000.01, wantCent: 0, wantOK: false},
		{name: "5000 边界放行", yuan: 5000, wantCent: 500000, wantOK: true},
		// 0.29*100 实际是 28.999999999999996,必须 round 到 29 而不是截断成 28
		{name: "浮点误差修正", yuan: 0.29, wantCent: 29, wantOK: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := agentTopUpAmountCents(c.yuan)
			if ok != c.wantOK || got != c.wantCent {
				t.Fatalf("agentTopUpAmountCents(%v) = (%d, %v), want (%d, %v)", c.yuan, got, ok, c.wantCent, c.wantOK)
			}
		})
	}
}

// 认领直达链接是游客唯一的入账入口: 链接丢了 token 就等于钱到不了账,
// 这里把"结尾斜杠/空格/空订单号"三种脏输入钉死。
func TestBuildAgentClaimUrl(t *testing.T) {
	cases := []struct {
		name     string
		server   string
		token    string
		tradeNo  string
		wantPath string
	}{
		{
			name:     "标准站点地址",
			server:   "https://scheng.net",
			token:    "244bafff3843a0a6566e99b6abc7e8a4",
			tradeNo:  "WXAGT20260925022759EJN1UIxw3A",
			wantPath: "https://scheng.net/agent?claim_token=244bafff3843a0a6566e99b6abc7e8a4&out_trade_no=WXAGT20260925022759EJN1UIxw3A",
		},
		{
			name:     "结尾斜杠不重复",
			server:   "https://scheng.net/",
			token:    "abc",
			tradeNo:  "no1",
			wantPath: "https://scheng.net/agent?claim_token=abc&out_trade_no=no1",
		},
		{
			name:     "无订单号时只带 token(前端用 token 兜底去重键)",
			server:   "https://scheng.net",
			token:    "abc",
			tradeNo:  "",
			wantPath: "https://scheng.net/agent?claim_token=abc",
		},
		{
			name:     "首尾空格与空白订单号被吃掉",
			server:   " https://scheng.net ",
			token:    " abc ",
			tradeNo:  "   ",
			wantPath: "https://scheng.net/agent?claim_token=abc",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAgentClaimUrl(c.server, c.token, c.tradeNo); got != c.wantPath {
				t.Fatalf("want %q, got %q", c.wantPath, got)
			}
		})
	}
}
