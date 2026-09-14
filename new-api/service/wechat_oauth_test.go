package service

import (
	"strings"
	"testing"
)

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func TestIsSafeLocalRedirect(t *testing.T) {
	cases := map[string]bool{
		"/wallet":               true,
		"/dashboard?tab=wallet": true,
		"/agent-chat":           true,
		"//evil.com":            false,
		"https://evil.com":      false,
		"http://evil.com/x":     false,
		"javascript:alert(1)":   false,
		"\\evil.com":            false,
		"/%2f%2fevil.com":       false,
		"":                      false,
	}
	for in, want := range cases {
		if got := IsSafeLocalRedirect(in); got != want {
			t.Errorf("IsSafeLocalRedirect(%q)=%v want %v", in, got, want)
		}
	}
}

func TestBuildWechatOauthAuthorizeURL(t *testing.T) {
	u := BuildWechatOauthAuthorizeURL("state-xyz")
	if u == "" {
		t.Fatal("authorize url empty")
	}
	// ponytail: 静默授权必须是 snsapi_base(不弹授权页),且带 state 防 CSRF、redirect_uri 必须编码。
	if !contains(u, "scope=snsapi_base") || !contains(u, "state=state-xyz") ||
		!contains(u, "response_type=code") || !contains(u, "redirect_uri=") {
		t.Fatalf("authorize url missing required params: %s", u)
	}
}
