package antchain

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
	"gitlab.alipay-inc.com/antchain/restclient-go-sdk/response"
)

// SubmitEvidence executes the three-step antchain evidence flow:
// insertOrder → completeOrder → logOrder. Any step failure aborts subsequent
// steps and returns an error (caller logs via SysError, no retry).
func SubmitEvidence(in model.SubmitOrderEvidenceInput) error {
	if restClient == nil {
		return fmt.Errorf("antchain client not initialized")
	}

	// 蚂蚁链 BaaS 同 account 高频调用限流(211), 三步串发易撞: step 间留间隔躲限流。
	// 生产 fire-and-forget goroutine 调, sleep 只拖 goroutine 不阻塞支付回调。
	const stepInterval = 3 * time.Second

	// 成功路径留痕: fire-and-forget 默认静默无法判上链否, 每步 SysLog trace tradeNo,
	// 完成打 PASS。失败仍由上层 SysError。便于 docker logs 验证(从前日志零 antchain 行无判)。
	traceTag := "antchain evidence[" + in.TradeNo + "]"

	// Step 1: insertOrder(tradeNo, userId, moneyFen, planId, provider)
	if err := callContract(
		"insertOrder(string,string,string,string,string)",
		[]string{in.TradeNo, in.UserId, in.MoneyFen, in.PlanId, in.Provider},
		"[]", false,
	); err != nil {
		return fmt.Errorf("insertOrder: %w", err)
	}
	common.SysLog(traceTag + " step1 insertOrder ok")
	time.Sleep(stepInterval)

	// Step 2: completeOrder(tradeNo, payTime, status, dataHash, bizType)
	if err := callContract(
		"completeOrder(string,string,string,string,string)",
		[]string{in.TradeNo, in.PayTime, in.Status, in.DataHash, in.BizType},
		"[]", false,
	); err != nil {
		return fmt.Errorf("completeOrder: %w", err)
	}
	common.SysLog(traceTag + " step2 completeOrder ok")
	time.Sleep(stepInterval)

	// Step 3: logOrder(tradeNo, browserJson) — emits LOG_STRING event for
	// antchain explorer searchability.
	if err := callContract(
		"logOrder(string,string)",
		[]string{in.TradeNo, in.EvidenceJSON},
		"[]", false,
	); err != nil {
		return fmt.Errorf("logOrder: %w", err)
	}
	common.SysLog(traceTag + " step3 logOrder ok")
	time.Sleep(stepInterval)

	// Step 4 (段2b): deliverOrder — 发货/履约存证, 复刻付款 in 全字段成完整单据(独立可验)。
	// deliveredAt 取此刻(≈quota 到账后)。与付款三步同 goroutine 串行 fire-and-forget,
	// 失败仅返回 err 由上层 SysError, 不回滚扣款。
	if err := DeliverOrder(in); err != nil {
		return fmt.Errorf("deliverOrder: %w", err)
	}
	common.SysLog(traceTag + " step4 deliverOrder ok — ALL STEPS PASS")

	return nil
}

// DeliverOrder 提交段2b 发货/履约存证到蚂蚁链。付费侧 in 复刻 9 字段 + deliveredAt + deliveryHash,
// DeliverJSON 带 11 英文锁字段 + 中文释义易读层 emit。独立函数便于 payment 回调既可经
// SubmitEvidence 串四步, 也可单独 fire-and-forget 调。
func DeliverOrder(in model.SubmitOrderEvidenceInput) error {
	if restClient == nil {
		return fmt.Errorf("antchain client not initialized")
	}
	deliveredAt := time.Now().Format(time.RFC3339)
	dv := model.BuildDeliverEvidence(in, deliveredAt)
	return callContract(
		"deliverOrder(string,string,string,string)",
		[]string{dv.TradeNo, dv.DeliverJSON, dv.DeliveredAt, dv.DeliveryHash},
		"[]", false,
	)
}

// callContract wraps restClient.CallContract with orderId generation and
// response validation.
func callContract(methodSig string, args []string, outTypes string, isLocal bool) error {
	// Marshal args as JSON array string: ["a","b","c"]
	paramBytes, err := common.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}

	orderId := fmt.Sprintf("ev_%s", uuid.New().String())
	var resp response.BaseResp
	resp, err = restClient.CallContract(
		bizId,
		orderId,
		account,
		tenantId,
		contractName,
		methodSig,
		string(paramBytes),
		outTypes,
		kmsId,
		isLocal,
		gas,
	)
	if err != nil {
		return fmt.Errorf("call %s: %w", methodSig, err)
	}
	// 写入链上回 Code="200" Success=true 表示 tx 接受; 只读 localTransaction 回 Code="0"
	// Success=true (errorCode 内嵌 SUCCESS)。两种都算成功。写入真 revert 网关回
	// Code="40x" Success=false + Data 嵌 SERVICE_TX_VERIFY_FAILED — 这是失败, 走 else。
	if !resp.Success || (resp.Code != "200" && resp.Code != "0") {
		return fmt.Errorf("call %s: code=%s data=%s", methodSig, resp.Code, resp.Data)
	}
	return nil
}

// demo 非导出自检 (design.md line169 ponytail 惯例): 打印一笔构造订单的三步参数组装结果,
// 便于人眼核验字段映射与 dataHash 一致性。不真打链 (restClient==nil 时 SubmitEvidence 直接报错)。
func demo() {
	in := model.SubmitOrderEvidenceInput{
		TradeNo:      "DEMO-001",
		UserId:       "42",
		MoneyFen:     "999",
		PlanId:       "3",
		Provider:     "alipay",
		PayTime:      "2026-07-18T12:00:00+08:00",
		Status:       "SUCCESS",
		DataHash:     "deadbeef",
		BizType:      "subscription",
		EvidenceJSON: "{}",
	}
	err := SubmitEvidence(in)
	fmt.Printf("demo SubmitEvidence: err=%v (nil-client 预期报错) input=%+v\n", err, in)
}
