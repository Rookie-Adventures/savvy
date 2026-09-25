package service

// 网关级契约测试(默认 skip)。这是唯一能证明「Go 侧 SkillHub 签名口径与微信
// AI 支付网关兼容」的手段 —— 单测只能证明自洽,证不了与对端口径一致。
//
// 运行方式(凭证从环境注入,绝不写死、绝不提交):
//   X402_LIVE=1 \
//   X402_LIVE_DEV_ID=sh-XXXX \
//   X402_LIVE_PUB_KEY_ID=PUB_KEY_XXXX \
//   X402_LIVE_PRIV_PEM_FILE=/path/dev_key.pem \
//   go test ./service/ -run TestX402PreorderAgainstGateway -v
//
// 用假 code_url:已证网关 ③ 步不校验签名与订单真实性,只校验报文格式与签名,
// 因此能拿到 payment_code 即说明签名口径正确。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestX402PreorderAgainstGateway(t *testing.T) {
	if os.Getenv("X402_LIVE") != "1" {
		t.Skip("set X402_LIVE=1 with SkillHub developer credentials to run")
	}
	pemFile := os.Getenv("X402_LIVE_PRIV_PEM_FILE")
	pemBytes, err := os.ReadFile(pemFile)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	operation_setting.X402Enabled = true
	operation_setting.SkillhubDeveloperId = os.Getenv("X402_LIVE_DEV_ID")
	operation_setting.SkillhubPubKeyId = os.Getenv("X402_LIVE_PUB_KEY_ID")
	operation_setting.SkillhubPrivateKeyPEM = string(pemBytes)
	operation_setting.X402SkillId = os.Getenv("X402_LIVE_SKILL_ID")
	if operation_setting.X402SkillId == "" {
		operation_setting.X402SkillId = "savvy-quota-topup"
	}
	operation_setting.X402SkillVersion = "1.0.0"
	operation_setting.X402AmountCents = 100

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	fakeCodeURL := "weixin://wxpay/bizpayurl?pr=X402GOTEST" + hex.EncodeToString(buf)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	code, expiresAt, xerr := X402Preorder(ctx, fakeCodeURL)
	if xerr != nil {
		t.Fatalf("preorder failed: %v", xerr)
	}
	if code == "" {
		t.Fatal("empty payment_code")
	}
	if expiresAt <= time.Now().Unix() {
		t.Fatalf("bad expires_at: %d", expiresAt)
	}
	t.Logf("payment_code=%s len=%d expires_in=%ds", code[:8]+"...", len(code), expiresAt-time.Now().Unix())
}
