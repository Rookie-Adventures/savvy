package controller

import "testing"

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
