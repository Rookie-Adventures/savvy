//go:build manual

package antchain

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// TestSubmitEvidence_E2E walks the three-step antchain evidence flow against
// the real contract: insertOrder → completeOrder → logOrder. Verifies the
// ABI encoding / contract semantics / LOG_STRING event layers that TestShake
// (auth only) does not cover.
//
// Run (bash, Windows paths need single quotes to survive backslash):
//
//	export ANTCHAIN_ENABLED=true
//	export ANTCHAIN_ACCESS_ID=pnv3kEhXTWHRFGOY
//	export ANTCHAIN_ACCESS_SECRET_FILE='E:\mayilian\access.key'
//	export ANTCHAIN_REST_URL=https://rest.baas.alipay.com
//	export ANTCHAIN_KMS_ID=7ysf2UgpTWHRFGOY1783011006931
//	export ANTCHAIN_ACCOUNT=savvy
//	export ANTCHAIN_CONTRACT_NAME=savvy-solidity
//	go test -run TestSubmitEvidence_E2E -tags=manual -v ./service/antchain/
//
// On pass, copy the printed TradeNo and search it on the antchain explorer →
// expect a LOG_STRING event carrying the evidence JSON.
//
// IMPORTANT: leave the .key path exactly as above when running on the dev
// box. On machine B the file is mounted at /secrets/antchain-access.key
// (chmod 600) and the env var flips to that. Do NOT commit real env values.
func TestSubmitEvidence_E2E(t *testing.T) {
	Init()
	if !Enabled {
		t.Skip("antchain Init did not enable (env not set / access.key missing); skipping E2E")
	}
	if restClient == nil || restClient.RestToken == "" {
		t.Fatal("Init reported Enabled but client/token missing — Init invariants off")
	}

	// Build input via the real topup mapping fn so field format matches prod。
	topup := &model.TopUp{
		TradeNo:         fmt.Sprintf("E2E-%d", time.Now().Unix()),
		UserId:          42,
		Money:           9.99,
		PaymentProvider: "alipay",
		CompleteTime:    time.Now().Unix(),
	}
	in := model.BuildTopupEvidence(topup)
	t.Logf(">>> 浏览器查此 tradeNo: %s", in.TradeNo)
	t.Logf("moneyFen=%s dataHash=%s", in.MoneyFen, in.DataHash)
	t.Logf("evidenceJSON=\n%s", in.EvidenceJSON)

	// 三步上链。任一步失败即停后续 (同 prod SubmitEvidence)。
	// 失败排查面只剩 ABI编码/合约语义/事件三层 (认证已由 TestShake 排除)。
	if err := SubmitEvidence(in); err != nil {
		t.Fatalf("SubmitEvidence failed: %v\n"+
			"排查三层见 docs/records/2026-07-18-antchain-evidence-logrus-and-key-deploy.md L111-113",
			err)
	}

	t.Logf("PASS: insertOrder+completeOrder+logOrder 全通, tradeNo=%s", in.TradeNo)
}

// TestQueryTradeNo_E2E 调用合约只读函数 getTradeNo(string)→string 隔离"写失败 vs 调用本身失败"。
// getTradeNo 无 onlyOwner、无 require, 应当无脑成功 (读空串为新 tradeNo)。
// 通过 → SDK 调合约端到端通, insertOrder 的 408+110 死圈在写入语义层。
// 通过的法门: outTypes 必须传 ["string"], 不能 []; 写入可 [], 读取要具体 type 列表。
func TestQueryTradeNo_E2E(t *testing.T) {
	Init()
	if !Enabled || restClient == nil {
		t.Skip("antchain not enabled; skipping")
	}
	tradeNo := fmt.Sprintf("Q-%d", time.Now().Unix())
	if err := callContract(
		"getTradeNo(string)",
		[]string{tradeNo},
		`["string"]`,
		true, // isLocal = 只读, 不上链
	); err != nil {
		t.Fatalf("getTradeNo read failed: %v", err)
	}
	t.Logf("PASS: getTradeNo(%s) 只读调用通, outTypes=[\"string\"]", tradeNo)
}

// keep os import referenced even if future edits drop the env reads above.
var _ = os.Getenv
