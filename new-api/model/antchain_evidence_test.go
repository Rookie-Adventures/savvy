package model

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func TestBuildSubscriptionEvidence_MoneyFen(t *testing.T) {
	tests := []struct {
		name     string
		money    float64
		wantFen  string
	}{
		{"9.99", 9.99, "999"},
		{"100.00", 100.00, "10000"},
		{"0.01", 0.01, "1"},
		{"19.90", 19.90, "1990"},
		{"rounding edge 9.995", 9.995, "999"}, // float64: 9.995 ≈ 9.994999... → *100=999.5→Round=999
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := &SubscriptionOrder{
				TradeNo:         "SUB-TEST",
				UserId:          1,
				Money:           tt.money,
				PlanId:          2,
				PaymentProvider: "alipay",
				CompleteTime:    time.Now().Unix(),
			}
			in := buildSubscriptionEvidence(order)
			if in.MoneyFen != tt.wantFen {
				t.Errorf("MoneyFen = %q, want %q", in.MoneyFen, tt.wantFen)
			}
			if in.BizType != "subscription" {
				t.Errorf("BizType = %q, want subscription", in.BizType)
			}
			if in.Status != "SUCCESS" {
				t.Errorf("Status = %q, want SUCCESS", in.Status)
			}
		})
	}
}

func TestBuildTopupEvidence_MoneyFen(t *testing.T) {
	topup := &TopUp{
		TradeNo:         "TOP-TEST",
		UserId:          42,
		Money:           50.00,
		PaymentProvider: "epay",
		CompleteTime:    time.Now().Unix(),
	}
	in := BuildTopupEvidence(topup)
	if in.MoneyFen != "5000" {
		t.Errorf("MoneyFen = %q, want 5000", in.MoneyFen)
	}
	if in.BizType != "topup" {
		t.Errorf("BizType = %q, want topup", in.BizType)
	}
	if in.PlanId != "" {
		t.Errorf("PlanId = %q, want empty for topup", in.PlanId)
	}
}

func TestCanonicalJSON_DataHash(t *testing.T) {
	// Verify dataHash is deterministic and excludes itself.
	m := map[string]string{
		"tradeNo":  "T1",
		"userId":   "1",
		"moneyFen": "999",
	}
	cj := canonicalJSON(m, "dataHash")
	h := sha256.Sum256(cj)
	hash1 := hex.EncodeToString(h[:])

	// Same input → same hash
	cj2 := canonicalJSON(m, "dataHash")
	h2 := sha256.Sum256(cj2)
	hash2 := hex.EncodeToString(h2[:])
	if hash1 != hash2 {
		t.Error("canonicalJSON not deterministic")
	}

	// Different input → different hash
	m["tradeNo"] = "T2"
	cj3 := canonicalJSON(m, "dataHash")
	h3 := sha256.Sum256(cj3)
	hash3 := hex.EncodeToString(h3[:])
	if hash1 == hash3 {
		t.Error("different input produced same hash")
	}
}

// TestBuildDeliverEvidence 验段2b 完整发货单据: 11 英文锁字段 + 中文释义易读层,
// deliveryHash 锁前10字段(付款9+deliveredAt, 不含 deliveryHash 自身), 付款字段篡改即废。
func TestBuildDeliverEvidence(t *testing.T) {
	in := SubmitOrderEvidenceInput{
		TradeNo:      "E2E-001",
		UserId:       "42",
		MoneyFen:     "999",
		PlanId:       "3",
		Provider:     "alipay",
		PayTime:      "2026-07-20T10:00:00+08:00",
		Status:       "SUCCESS",
		BizType:      "topup",
		DataHash:     "deadbeef",
		EvidenceJSON: "{}",
	}
	dv := BuildDeliverEvidence(in, "2026-07-20T12:00:00+08:00")

	// 1. deliveryHash 确定性: 同输入必相等。
	dv2 := BuildDeliverEvidence(in, "2026-07-20T12:00:00+08:00")
	if dv.DeliveryHash != dv2.DeliveryHash {
		t.Fatal("deliveryHash not deterministic on identical input")
	}
	// 2. 付款字段篡改必废 hash(证明锁了付款侧, 不只 deliveredAt)。
	in2 := in
	in2.MoneyFen = "1000"
	dv3 := BuildDeliverEvidence(in2, "2026-07-20T12:00:00+08:00")
	if dv.DeliveryHash == dv3.DeliveryHash {
		t.Fatal("deliveryHash insensitive to moneyFen change — 付款字段未锁")
	}
	// 3. deliveredAt 篡改必废 hash。
	dv4 := BuildDeliverEvidence(in, "2026-07-20T13:00:00+08:00")
	if dv.DeliveryHash == dv4.DeliveryHash {
		t.Fatal("deliveryHash insensitive to deliveredAt change")
	}

	// 4. DeliverJSON 必须合法 JSON, 11 中文锁键全在(全进 hash, 一键一值不重复)。
	var parsed map[string]any
	if err := common.Unmarshal([]byte(dv.DeliverJSON), &parsed); err != nil {
		t.Fatalf("DeliverJSON not valid JSON: %v\n%s", err, dv.DeliverJSON)
	}
	lockedKeys := []string{
		"交易号", "用户ID", "付款金额_分", "套餐ID", "支付渠道",
		"付款时间", "付款状态", "业务类型", "付款指纹", "发货时间", "发货指纹",
		"收款主体", "收款域名",
	}
	for _, k := range lockedKeys {
		if _, ok := parsed[k]; !ok {
			t.Errorf("DeliverJSON missing 中文锁键 %q", k)
		}
	}
	// 中文锁键值正确(取关键样本)。
	if parsed["付款金额_分"] != "999" || parsed["交易号"] != "E2E-001" {
		t.Errorf("DeliverJSON 中文键值错: %+v", parsed)
	}
	// 英文旧键不应残留(确认脱英文壳)。
	for _, oldK := range []string{"tradeNo", "userId", "moneyFen", "dataHash"} {
		if _, ok := parsed[oldK]; ok {
			t.Errorf("DeliverJSON 残留英文键 %q — 应全中文", oldK)
		}
	}
}

func TestMoneyFenConversion_FloatPrecision(t *testing.T) {
	// Edge case: float64 * 100 can produce 998.99999... instead of 999
	money := 9.99
	fen := strconv.FormatInt(int64(math.Round(money*100)), 10)
	if fen != "999" {
		t.Errorf("expected 999, got %s", fen)
	}
}
