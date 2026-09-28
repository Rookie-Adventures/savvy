package operation_setting

import "testing"

// X402 收银台与 AI 问答履约是两套依赖: 早期被绑成一道门槛,
// 结果只开通充值也必须先凑齐并且可能没有的 relay token, /api/skill/invoke 直接 503。
// 这里把「充值够用」与「问答够用」钉死,别再让两者互相拖累。
func TestSkillPayConfigGates(t *testing.T) {
	snap := struct {
		enabled       bool
		priceFen      int
		skillID       string
		developerID   string
		pubKeyID      string
		privateKeyPEM string
		relayToken    string
		relayModel    string
	}{enabled: SkillPayEnabled, priceFen: SkillPayPriceFen, skillID: SkillPaySkillId,
		developerID: SkillPayDeveloperId, pubKeyID: SkillPayPubKeyId,
		privateKeyPEM: SkillPayPrivateKeyPEM, relayToken: SkillPayRelayToken, relayModel: SkillPayRelayModel}
	defer func() {
		SkillPayEnabled, SkillPayPriceFen, SkillPaySkillId = snap.enabled, snap.priceFen, snap.skillID
		SkillPayDeveloperId, SkillPayPubKeyId, SkillPayPrivateKeyPEM = snap.developerID, snap.pubKeyID, snap.privateKeyPEM
		SkillPayRelayToken, SkillPayRelayModel = snap.relayToken, snap.relayModel
	}()

	set := func(enabled bool, priceFen int, skillID, devID, pubKeyID, pem, token, model string) {
		SkillPayEnabled, SkillPayPriceFen, SkillPaySkillId = enabled, priceFen, skillID
		SkillPayDeveloperId, SkillPayPubKeyId, SkillPayPrivateKeyPEM = devID, pubKeyID, pem
		SkillPayRelayToken, SkillPayRelayModel = token, model
	}

	cases := []struct {
		name      string
		fields    func()
		wantCore  bool
		wantRelay bool
	}{
		{
			name: "五项齐全(开通:Agent 充值)→ 收银台可用,问答仍不可用",
			fields: func() {
				set(true, 10, "savvy-quota-topup", "sh-xxx", "PUB_KEY_xxx", "-----BEGIN PRIVATE KEY-----", "", "")
			},
			wantCore:  true,
			wantRelay: false,
		},
		{
			name:      "七项齐全 → 两条路径都可用",
			fields:    func() { set(true, 10, "savvy-quota-topup", "sh-xxx", "PUB_KEY_xxx", "PEM", "sk-relay", "gpt-4o-mini") },
			wantCore:  true,
			wantRelay: true,
		},
		{
			name:      "总开关关闭 → 全否(保持 503 行为)",
			fields:    func() { set(false, 10, "savvy-quota-topup", "sh-xxx", "PUB_KEY_xxx", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "价格为 0 → 收银台不可用",
			fields:    func() { set(true, 0, "savvy-quota-topup", "sh-xxx", "PUB_KEY_xxx", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "缺私钥 → 收银台不可用",
			fields:    func() { set(true, 10, "savvy-quota-topup", "sh-xxx", "PUB_KEY_xxx", "", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "缺 skill_id → 收银台不可用",
			fields:    func() { set(true, 10, "", "sh-xxx", "PUB_KEY_xxx", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "缺 pub_key_id → 收银台不可用",
			fields:    func() { set(true, 10, "savvy-quota-topup", "sh-xxx", "", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "缺 developer_id → 收银台不可用",
			fields:    func() { set(true, 10, "savvy-quota-topup", "", "PUB_KEY_xxx", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.fields()
			if got := IsSkillPayConfigured(); got != c.wantCore {
				t.Fatalf("IsSkillPayConfigured() = %v, want %v", got, c.wantCore)
			}
			if got := IsSkillPayRelayConfigured(); got != c.wantRelay {
				t.Fatalf("IsSkillPayRelayConfigured() = %v, want %v", got, c.wantRelay)
			}
		})
	}
}

// SKILLPAY_SKILL_ID 只接受 SkillHub 发布的 slug。2026-09-29 对照官方社区元技能
// (@user_4894573e/skill-paid v3.1.1 config_collection_checklist) 发现我们把发布用 Token
// (bt_ 前缀, 官方标 🔴敏感/"绝不写入 Skill 包")当成了 skill_id —— 它会被写进 L2 参与签名
// 并发往预下单端点。凭据形状一律装载时置空:宁可 /api/skill/invoke 回 503,也不能把凭据当配置外发。
func TestSkillPaySkillIdRejectsCredentialShape(t *testing.T) {
	snap := SkillPaySkillId
	defer func() { SkillPaySkillId = snap }()

	for _, bad := range []string{"bt_n2cz4sedn8vxxt339te9pz9vjwm8yfap", "sh-8pTcew4S", "PUB_KEY_BC103C9F"} {
		SkillPaySkillId = bad
		if looksLikeSkillHubCredential(bad) == false {
			t.Fatalf("凭据形状未识别: %s", bad)
		}
	}
	for _, ok := range []string{"savvy-quota-topup", "savvy-ai-qa", ""} {
		if looksLikeSkillHubCredential(ok) {
			t.Fatalf("slug 被误判为凭据: %q", ok)
		}
	}

	t.Setenv("SKILLPAY_ENABLED", "true")
	t.Setenv("SKILLPAY_PRICE_FEN", "10")
	t.Setenv("SKILLPAY_SKILL_ID", "bt_n2cz4sedn8vxxt339te9pz9vjwm8yfap")
	t.Setenv("SKILLPAY_DEVELOPER_ID", "sh-xxx")
	t.Setenv("SKILLPAY_PUB_KEY_ID", "PUB_KEY_xxx")
	t.Setenv("SKILLPAY_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----")
	InitSkillPayFromEnv()
	if SkillPaySkillId != "" {
		t.Fatalf("Token 形态的 skill_id 应被置空，实际 %q", SkillPaySkillId)
	}
	if IsSkillPayConfigured() {
		t.Fatalf("skill_id 被拒后收银台必须判为未配置(回 503)，不能带着凭据继续签名")
	}
}
