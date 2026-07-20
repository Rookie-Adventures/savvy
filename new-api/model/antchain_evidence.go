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

// DeliverEvidenceInput 段2b 发货/履约存证输入: 链证合一条完整中文单据(独立可验, 不依赖
// 付款侧 logOrder 那条), deliveredAt 取发货时刻, deliveryHash 锁前 10 中文锁字段
// (不含 发货指纹 自身)。“付款指纹”= 付款侧 dataHash 原串复刻(英文算法算出的锁值,
// 链住付款 8 字段锁, 跨条连贯)。
type DeliverEvidenceInput struct {
	TradeNo      string
	UserId       string
	MoneyFen     string
	PlanId       string
	Provider     string
	PayTime      string
	Status       string
	BizType      string
	DataHash     string // 付款侧 dataHash 原串复刻(英文算法产出)
	DeliveredAt  string
	DeliveryHash string
	DeliverJSON  string // emit LOG_STRING 用, 11 中文锁键 JSON
}

// BuildDeliverEvidence 构造段2b 完整发货中文单据。付费侧 in 复刻(付款指纹 = 付款侧 dataHash 原串),
// deliveredAt 传发货时刻 ISO8601。deliveryHash = SHA-256(canonicalJSON(前10中文锁字段,
// 不含发货指纹自身)) — 铁锁: 改任一中文锁字段 key 名/值即废链证。
// ponytail: 中文键全进 canonicalJSON, 一份键一份值, 不重复。
func BuildDeliverEvidence(in SubmitOrderEvidenceInput, deliveredAt string) DeliverEvidenceInput {
	// 11 中文锁字段。键全中文(链上单据易读, exploreredump 不再英中交错), 全进 deliveryHash。
	locked := map[string]string{
		"交易号":    in.TradeNo,
		"用户ID":   in.UserId,
		"付款金额_分": in.MoneyFen,
		"套餐ID":   in.PlanId,
		"支付渠道":   in.Provider,
		"付款时间":   in.PayTime,
		"付款状态":   in.Status,
		"业务类型":   in.BizType,
		"付款指纹":   in.DataHash, // 段2a付款侧英文算法锁值(链住付款8字段)
		"发货时间":   deliveredAt,
	}
	deliveryHash := sha256Hex(canonicalJSON(locked, "")) // 锁前10字段(不含发货指纹自身)
	locked["发货指纹"] = deliveryHash

	// 权威段(不进 deliveryHash 锁面): 写死的收款主体+域名, 链上一眼读出"这单由谁收"。
	// 写死值锁它无验用价值(自己填自己锁=自我证明, 非第三方不可抵赖), 留在不进 hash 的展示层。
	locked["收款主体"] = "郑州市管城回族区栗橙网络科技工作室(个体工商户)"
	locked["收款域名"] = "scheng.net"

	delivered := DeliverEvidenceInput{
		TradeNo:      in.TradeNo,
		UserId:       in.UserId,
		MoneyFen:     in.MoneyFen,
		PlanId:       in.PlanId,
		Provider:     in.Provider,
		PayTime:      in.PayTime,
		Status:       in.Status,
		BizType:      in.BizType,
		DataHash:     in.DataHash,
		DeliveredAt:  deliveredAt,
		DeliveryHash: deliveryHash,
		DeliverJSON:  prettyJSON(locked), // 全中文键, 一键一值
	}
	return delivered
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
