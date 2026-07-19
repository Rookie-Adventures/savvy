package model

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
	"testing"
	"time"
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

func TestMoneyFenConversion_FloatPrecision(t *testing.T) {
	// Edge case: float64 * 100 can produce 998.99999... instead of 999
	money := 9.99
	fen := strconv.FormatInt(int64(math.Round(money*100)), 10)
	if fen != "999" {
		t.Errorf("expected 999, got %s", fen)
	}
}
