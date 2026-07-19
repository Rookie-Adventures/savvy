package model

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// buildSubscriptionEvidence maps a SubscriptionOrder to SubmitOrderEvidenceInput.
// Called synchronously to build the input, then passed to the async goroutine.
func buildSubscriptionEvidence(order *SubscriptionOrder) SubmitOrderEvidenceInput {
	userId := strconv.Itoa(order.UserId)
	moneyFen := strconv.FormatInt(int64(math.Round(order.Money*100)), 10)
	planId := strconv.Itoa(order.PlanId)
	payTime := time.Unix(order.CompleteTime, 0).Format(time.RFC3339)

	fields := map[string]string{
		"bizType":  "subscription",
		"dataHash": "", // filled below
		"moneyFen": moneyFen,
		"payTime":  payTime,
		"planId":   planId,
		"provider": order.PaymentProvider,
		"status":   "SUCCESS",
		"tradeNo":  order.TradeNo,
		"userId":   userId,
	}

	// dataHash = SHA-256(canonicalJSON(fields without dataHash))
	dataHash := sha256Hex(canonicalJSON(fields, "dataHash"))
	fields["dataHash"] = dataHash

	return SubmitOrderEvidenceInput{
		TradeNo:      order.TradeNo,
		UserId:       userId,
		MoneyFen:     moneyFen,
		PlanId:       planId,
		Provider:     order.PaymentProvider,
		PayTime:      payTime,
		Status:       "SUCCESS",
		DataHash:     dataHash,
		BizType:      "subscription",
		EvidenceJSON: prettyJSON(fields),
	}
}

// BuildTopupEvidence maps a TopUp to SubmitOrderEvidenceInput.
// Exported for use by controller package.
func BuildTopupEvidence(topup *TopUp) SubmitOrderEvidenceInput {
	userId := strconv.Itoa(topup.UserId)
	moneyFen := strconv.FormatInt(int64(math.Round(topup.Money*100)), 10)
	payTime := time.Unix(topup.CompleteTime, 0).Format(time.RFC3339)

	fields := map[string]string{
		"bizType":  "topup",
		"dataHash": "",
		"moneyFen": moneyFen,
		"payTime":  payTime,
		"planId":   "",
		"provider": topup.PaymentProvider,
		"status":   "SUCCESS",
		"tradeNo":  topup.TradeNo,
		"userId":   userId,
	}

	dataHash := sha256Hex(canonicalJSON(fields, "dataHash"))
	fields["dataHash"] = dataHash

	return SubmitOrderEvidenceInput{
		TradeNo:      topup.TradeNo,
		UserId:       userId,
		MoneyFen:     moneyFen,
		PlanId:       "",
		Provider:     topup.PaymentProvider,
		PayTime:      payTime,
		Status:       "SUCCESS",
		DataHash:     dataHash,
		BizType:      "topup",
		EvidenceJSON: prettyJSON(fields),
	}
}

// canonicalJSON serializes a string map with sorted keys, optionally excluding
// one key (used to compute dataHash over all fields except dataHash itself).
// IMPORTANT: This algorithm is locked once evidence is on-chain. Changing it
// breaks the evidence chain — on-chain hash won't match recomputed hash.
func canonicalJSON(m map[string]string, excludeKey string) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == excludeKey {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build ordered JSON manually to guarantee key order regardless of Go map
	// iteration. json.Marshal on map[string]string does sort keys, but being
	// explicit here makes the contract undeniable.
	buf := make([]byte, 0, 256)
	buf = append(buf, '{')
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, _ := common.Marshal(k)
		vb, _ := common.Marshal(m[k])
		buf = append(buf, kb...)
		buf = append(buf, ':')
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return buf
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func prettyJSON(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := make([]byte, 0, 512)
	buf = append(buf, "{\n"...)
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ",\n"...)
		}
		kb, _ := common.Marshal(k)
		vb, _ := common.Marshal(m[k])
		buf = append(buf, "  "...)
		buf = append(buf, kb...)
		buf = append(buf, ": "...)
		buf = append(buf, vb...)
	}
	buf = append(buf, "\n}"...)
	return string(buf)
}
