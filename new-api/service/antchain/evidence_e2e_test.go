//go:build manual

package antchain

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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
//	export ANTCHAIN_CONTRACT_NAME=OrderEvidence   # 链上注册名, 不是 savvy-solidity 编译文件名
//	export ANTCHAIN_GAS=500000                    # 写入上限, completeOrder需350000+, 0过小→10200/408
//	export ANTCHAIN_BIZ_ID=a00e36c5               # 必填(网关 41400)
//	export ANTCHAIN_TENANT_ID=TWHRFGOY            # 必填(网关 41400)
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

	t.Logf("PASS: 付款三步(insertOrder+completeOrder+logOrder)全通, tradeNo=%s", in.TradeNo)

	// 段2b 第四步: deliverOrder 发货/履约存证。
	// 前置: 链上合约须已重新部署含 deliverOrder 函数 + deliveredAt/deliveryHash 字段
	// (本合约改动需重新发布到 BaaS, 旧合约无此函数会 revert)。
	if err := DeliverOrder(in); err != nil {
		t.Fatalf("DeliverOrder failed: %v\n "+
			"前置检查: 合约是否已重新部署含 deliverOrder? (deliverOrder 不存在 → revert)",
			err)
	}
	t.Logf("PASS: 段2b deliverOrder 全通, tradeNo=%s, 全流程 付款8字段+发货2字段 链证闭合", in.TradeNo)
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

// TestQueryRealTrade_E2E 只读查询真实支付回调单是否上链成功。
// 查 getDeliveryHash(tradeNo) + getDeliveredAt(tradeNo):
//   - getTradeNo 有值 = insertOrder 写过(付款第一步上了)
//   - getDeliveredAt 有值 + getDeliveryHash 有值 = deliverOrder 第四步上了(段2b 成功)
// 没值则对应步骤没走通。run:
//
//	(同 TestSubmitEvidence_E2E 的 export 块)
//	go test -run TestQueryRealTrade_E2E -tags=manual -v ./service/antchain/ \
//	  -args -tradeNo=ALIPAYUSR2NOENFDxS1784560576
//
// 也可直接改 defaultTradeNo 常量省去 -args。
func TestQueryRealTrade_E2E(t *testing.T) {
	defaultTradeNo := "ALIPAYUSR2NOENFDxS1784560576"
	tradeNo := defaultTradeNo
	for i, a := range os.Args {
		if a == "-tradeNo" && i+1 < len(os.Args) {
			tradeNo = os.Args[i+1]
		}
	}
	Init()
	if !Enabled || restClient == nil {
		t.Skip("antchain not enabled; skipping")
	}
	t.Logf("查询合约 savvy1: tradeNo=%s", tradeNo)

	// 3 个只读 getter, 串查: 付款存证 + 发货存证是否在链。
	// 直接调 restClient 取 resp.Data (callContract 吞 Data), 看实际返回判空 vs 有值。
	reads := []struct{ sig, label string }{
		{"getTradeNo(string)", "交易号(付款insertOrder)"},
		{"getDeliveredAt(string)", "发货时间(deliverOrder第四步)"},
		{"getDeliveryHash(string)", "发货指纹(deliverOrder第四步)"},
	}
	for _, r := range reads {
		paramBytes, _ := common.Marshal([]string{tradeNo})
		resp, err := restClient.CallContract(
			bizId,
			fmt.Sprintf("q_%d", time.Now().UnixNano()),
			account, tenantId, contractName,
			r.sig, string(paramBytes), `["string"]`, kmsId,
			true, gas,
		)
		if err != nil {
			t.Errorf("%s 调用失败: %v", r.label, err)
			continue
		}
		t.Logf("%s → code=%s data=%q", r.label, resp.Code, resp.Data)
	}
}

// keep os import referenced even if future edits drop the env reads above.
var _ = os.Getenv
