package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
)

// handleWxNotify 封装微信 APIv3 通知:解密+验签用 SDK,拿 OutTradeNo 后调 finalize。
// finalize 失败返 5xx+FAIL JSON,成功返 200+SUCCESS JSON(微信不重试 SUCCESS)。
func handleWxNotify(c *gin.Context, finalize func(c *gin.Context, tradeNo, payload string) error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "read body"})
		return
	}
	// SDK 解密+验签拿明文(含 OutTradeNo):
	tradeNo, plaintext, err := decryptWxNativeNotify(body, c.Request.Header)
	if err != nil || tradeNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "verify/decrypt failed"})
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	if err := finalize(c, tradeNo, plaintext); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "FAIL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "OK"})
}

// wxNotifyContent 是微信支付 APIv3 通知解密后的业务明文结构。仅取 OutTradeNo;其余字段忽略。
type wxNotifyContent struct {
	OutTradeNo string `json:"out_trade_no"`
	// TransactionId, TradeState, Amount 等字段存在但本路径不消费(已 LockOrder 防重,CompleteSubscriptionOrder/inline finalize 做幂等)。
}

// decryptWxNativeNotify:用 wechatpay-go 解密+验签微信 APIv3 通知,返回 OutTradeNo + 明文 JSON。
// APIv3 通知=外层签名头(Wechatpay-Timestamp/Nonce/Serial/Signature)+ 内层 resource.ciphertext(AES-256-GCM)。
//
// ponytail: 真 SDK v0.2.21 形态(读模块缓存 core/notify/notify.go:156,64-91 定):
//   verifier = verifiers.NewSHA256WithRSAVerifier(downloader.MgrInstance().GetCertificateVisitor(mchID))
//     — 与 GetWechatClient 内 WithWechatPayAutoAuthCipher 用同一个 mgr 单例 → 同一证书源 → 验签与请求签名一致。
//   handler  = notify.NewRSANotifyHandler(apiV3Key, verifier)
//     — 内部 aes.NewCipher(apiV3Key)+cipher.NewGCM → AEAD,验签用 validator.Validate(签名头)。
//   handler.ParseNotifyRequest(ctx, httpReq, &content)
//     — 一次调用完成:读 Wechatpay-Signature-Type → suite.validator.Validate(签名头验签)
//       → json.Unmarshal 外层 → doAEADOpen(resource.ciphertext, AES-256-GCM 解密) → json.Unmarshal 明文到 content。
//   返回 (*notify.Request, error);content.OutTradeNo 已填;req.Resource.Plaintext 是完整明文 JSON。
//
// 签名头验签失败 → Validate 返 err → 本函数返 ("","",err) → handler 返 400 FAIL(非静默 OK)。
// 解密失败 → doAEADOpen 返 err → 同上。
// 验签通过但 OutTradeNo 空(异常明文)→ 本函数显式返 error,绝不静默返 ("",nil)。
//
// 绝不返回 ("", "", nil):空 tradeNo + nil err 会让 handleWxNotify 越过 err 检查走到 LockOrder("") 锁空串 →
// 静默接受未验签通知 = CRITICAL 安全洞。tradeNo=="" 即 err。
func decryptWxNativeNotify(body []byte, header http.Header) (tradeNo, plaintext string, err error) {
	// 重建 http.Request:SDK ParseNotifyRequest 读 request.Body + request.Header(签名头)。
	// ponytail: 不复用 c.Request.Body(gin 已消费);用已读 body 重建,c.Request.Header 透传签名头。
	req, err := http.NewRequest(http.MethodPost, "/", nil)
	if err != nil {
		return "", "", fmt.Errorf("new request: %w", err)
	}
	req.Header = header
	req.Body = io.NopCloser(bytes.NewReader(body))
	// verifier 源 = GetWechatClient 同一 mgr 单例(平台证书自动下载器)。与请求端验签同证书池。
	certVisitor := downloader.MgrInstance().GetCertificateVisitor(operation_setting.WechatMchID)
	verifier := verifiers.NewSHA256WithRSAVerifier(certVisitor)
	handler, err := notify.NewRSANotifyHandler(operation_setting.WechatAPIv3Key, verifier)
	if err != nil {
		return "", "", fmt.Errorf("init notify handler: %w", err)
	}
	var content wxNotifyContent
	notifyReq, err := handler.ParseNotifyRequest(context.Background(), req, &content)
	if err != nil {
		return "", "", fmt.Errorf("verify/decrypt notify: %w", err)
	}
	if content.OutTradeNo == "" {
		// 验签+解密都过了但明文没 OutTradeNo → 异常,显式报错(不静默通过)。
		return "", "", fmt.Errorf("notify plaintext missing out_trade_no")
	}
	if notifyReq != nil && notifyReq.Resource != nil {
		plaintext = notifyReq.Resource.Plaintext
	}
	if plaintext == "" {
		// 兜底:Resource.Plaintext 应已填,空则用 content 重建(保留语义:返回完整明文 JSON 给 finalize 落 ProviderPayload)。
		plaintext = common.GetJsonString(content)
	}
	return content.OutTradeNo, plaintext, nil
}
