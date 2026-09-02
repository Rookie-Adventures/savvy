package controller

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

// agentQuotaAmountFromMoney 是 getPayMoney 的逆运算: 实付 RMB → 额度单位数量。
// ponytail: 不套 AmountDiscount(那是预设金额档位的促销,智能体订单金额任意,无档位可配)。
// 返回单位与 TopUp.Amount 语义一致: 非 TOKENS 展示 = USD 整数; TOKENS 展示 = token 数,
// 保证认领/入账处 quotaToAdd = Amount * QuotaPerUnit 与现有 AlipayNotify 同构。
func agentQuotaAmountFromMoney(money float64, group string) int64 {
	dMoney := decimal.NewFromFloat(money)
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	// Price 被误配为 0 时 decimal.Div 会 panic,与 ratio 同款兜底
	price := operation_setting.Price
	if price == 0 {
		price = 1
	}
	usd := dMoney.Div(decimal.NewFromFloat(price)).
		Div(decimal.NewFromFloat(ratio))
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return usd.Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	return usd.Round(0).IntPart()
}

// newClaimToken 生成 128-bit 随机认领凭据(hex 32 字符)。
// 不用 common.GetRandomString: 认领凭据是钱的钥匙,必须 crypto/rand。
func newClaimToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
