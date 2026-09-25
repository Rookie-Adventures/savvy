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
			fields:    func() { set(true, 10, "bt_xxx", "sh-xxx", "PUB_KEY_xxx", "PEM", "sk-relay", "gpt-4o-mini") },
			wantCore:  true,
			wantRelay: true,
		},
		{
			name:      "总开关关闭 → 全否(保持 503 行为)",
			fields:    func() { set(false, 10, "bt_xxx", "sh-xxx", "PUB_KEY_xxx", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "价格为 0 → 收银台不可用",
			fields:    func() { set(true, 0, "bt_xxx", "sh-xxx", "PUB_KEY_xxx", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "缺私钥 → 收银台不可用",
			fields:    func() { set(true, 10, "bt_xxx", "sh-xxx", "PUB_KEY_xxx", "", "sk-relay", "m") },
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
			fields:    func() { set(true, 10, "bt_xxx", "sh-xxx", "", "PEM", "sk-relay", "m") },
			wantCore:  false,
			wantRelay: true,
		},
		{
			name:      "缺 developer_id → 收银台不可用",
			fields:    func() { set(true, 10, "bt_xxx", "", "PUB_KEY_xxx", "PEM", "sk-relay", "m") },
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
