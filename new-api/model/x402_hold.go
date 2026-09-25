package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
)

// X402 挂账认领单 — 微信 AI 支付(Pay Skill / X402)身份挂账队列。
//
// 设计:钱跟着订单走(与现有微信直连一致),本表只记"这笔钱该进谁的账户"。
// topup 行不在下单时创建——②步 Native 下单时 UserId 尚不存在;统一在身份
// 确认(BindX402Hold)那一刻补建 topup(status=success),避免 notify 与
// 认领双路加款。订单金额/价签由本行承载。
//
// 生命周期(对应协议九步):
//
//	pending   ④ 首次 invoke:Native 下单+预下单完成,等支付
//	held      ⑨ 查单 SUCCESS 但 agent 身份未确认 → 钱已收,挂账不入
//	credited  claim 绑定完成 → 补建 topup + 配额入账(队列排空)
//	expired   pending 超 15 分钟(payment_code 微信硬上限)→ 无资金损失,归档
//
// 金额口径:X402AmountCents 必须是整元(100 的整数倍),Amount(元)与 TopUp.Amount
// 同单位,配额 = Amount × QuotaPerUnit,与 notify finalize 完全一致;Money(实收元)
// 仅用于台账展示与对账。
type X402Hold struct {
	Id      int    `json:"id" gorm:"primaryKey"`
	TradeNo string `json:"trade_no" gorm:"column:trade_no;type:varchar(64);uniqueIndex;not null"` // WX402_*,≤32 微信硬限
	// PaymentCodeHash — ④步下发给 Agent 的 payment_code 的 SHA-256(hex)。⑦步重试
	// 必须回显同一个码才允许履约:商户单号是可枚举串,只认单号等于任何人猜到单号
	// 就能把别人的挂账认领走。存哈希而非明文,库里不留可用凭证。
	PaymentCodeHash string  `json:"-" gorm:"column:payment_code_hash;type:varchar(64);index;default:''"`
	AgentUserId     string  `json:"agent_user_id" gorm:"column:agent_user_id;type:varchar(64);index;default:''"`
	ClaimToken      string  `json:"-" gorm:"column:claim_token;type:varchar(32);uniqueIndex;not null"` // 一次性认领码
	Amount          int64   `json:"amount" gorm:"column:amount;bigint"`                                // 元(与 TopUp.Amount 同单位)
	Money           float64 `json:"money" gorm:"column:money"`                                         // 实收(元),= Amount(当前 1:1 定价)
	Status          string  `json:"status" gorm:"column:status;type:varchar(16);index;default:'pending'"`
	PayTime         int64   `json:"pay_time" gorm:"column:pay_time;default:0"`  // ⑧步查单 SUCCESS 时刻
	ExpiresAt       int64   `json:"expires_at" gorm:"column:expires_at;bigint"` // 创建+900s
	CreatedAt       int64   `json:"created_at" gorm:"column:created_at;bigint"`
	ClaimedAt       int64   `json:"claimed_at" gorm:"column:claimed_at;default:0"`
	ClaimedBy       int     `json:"claimed_by" gorm:"column:claimed_by;default:0"` // 首认 user id,审计
}

const (
	X402HoldStatusPending  = "pending"
	X402HoldStatusHeld     = "held"
	X402HoldStatusCredited = "credited"
	X402HoldStatusExpired  = "expired"

	X402HoldTTLSeconds = 900 // payment_code 微信硬上限,不可放宽
)

func (X402Hold) TableName() string { return "x402_holds" }

// 建表与补列由 migrateDB 的 DB.AutoMigrate(&X402Hold{}, &X402AgentBind{}) 负责
// (见 model/main.go),此处不再单独 Ensure —— 避免两处迁移互相打架。

func InsertX402Hold(h *X402Hold) error { return DB.Create(h).Error }

// GetX402HoldByTradeNo — ⑦⑧步定位挂账单;pending 过期读时即标 expired(免定时器)。
func GetX402HoldByTradeNo(tradeNo string) *X402Hold {
	if tradeNo == "" {
		return nil
	}
	var h X402Hold
	if DB.Where("trade_no = ?", tradeNo).First(&h).Error != nil {
		return nil
	}
	if h.Status == X402HoldStatusPending && h.ExpiresAt > 0 && common.GetTimestamp() > h.ExpiresAt {
		DB.Model(&X402Hold{}).Where("id = ?", h.Id).Update("status", X402HoldStatusExpired)
		h.Status = X402HoldStatusExpired
	}
	return &h
}

func ResolveX402HoldByClaim(token string) *X402Hold {
	if token == "" {
		return nil
	}
	var h X402Hold
	if DB.Where("claim_token = ?", token).First(&h).Error != nil {
		return nil
	}
	return &h
}

// ListX402HeldForUser — 该账号已绑定的 agent 身份下、钱已到账但尚未入账的挂账单。
// 前端用它提示「你有 N 元待认领」。claimed_by 用于回看历史入账。
func ListX402HeldForUser(userId int) ([]X402Hold, error) {
	var binds []X402AgentBind
	if err := DB.Where("user_id = ?", userId).Find(&binds).Error; err != nil {
		return nil, err
	}
	if len(binds) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(binds))
	for _, b := range binds {
		ids = append(ids, b.AgentUserId)
	}
	var list []X402Hold
	err := DB.Where("status = ? AND agent_user_id IN ?", X402HoldStatusHeld, ids).
		Order("id DESC").Limit(50).Find(&list).Error
	return list, err
}

// MarkX402Held — 款已到账 → 入队。pending→held 天然幂等;expired→held 是复活:
// payment_code 只有 15 分钟,但 Native code_url 有效期更长,用户可能在过期后才扫
// 码付款。只要微信确认扣款成功(notify 或查单 SUCCESS),这笔钱就必须能入账,不能
// 因本地 TTL 判定而吞掉。
func MarkX402Held(h *X402Hold) error {
	now := GetDBTimestamp()
	res := DB.Model(&X402Hold{}).
		Where("id = ? AND status IN ?", h.Id, []string{X402HoldStatusPending, X402HoldStatusExpired}).
		Updates(map[string]interface{}{"status": X402HoldStatusHeld, "pay_time": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		h.PayTime = now
	}
	h.Status = X402HoldStatusHeld
	return nil
}

// BindX402Hold — 认领排空队列:条件更新 held→credited 作幂等锁(双绑只放行一个),
// 随后补建 topup(success) + 加配额 + 台账。任一步失败回滚 hold 状态允许重试。
//
// 返回 credited=true 表示本次调用完成了入账;false 表示此前已入账(幂等命中)。
func BindX402Hold(h *X402Hold, userId int, callerIp string) (credited bool, err error) {
	if h == nil || userId <= 0 {
		return false, errors.New("invalid bind request")
	}
	if h.Status != X402HoldStatusHeld {
		return false, nil // pending/expired/credited 一律拒入队外状态;上层给文案
	}
	now := GetDBTimestamp()
	res := DB.Model(&X402Hold{}).
		Where("id = ? AND status = ?", h.Id, X402HoldStatusHeld).
		Updates(map[string]interface{}{"status": X402HoldStatusCredited, "claimed_at": now, "claimed_by": userId})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil // 并发认领:先者已入账
	}
	topUp := &TopUp{
		UserId:          userId,
		Amount:          h.Amount,
		Money:           h.Money,
		TradeNo:         h.TradeNo,
		PaymentMethod:   PaymentMethodWechat,
		PaymentProvider: PaymentProviderWechat,
		CreateTime:      h.CreatedAt,
		CompleteTime:    now,
		Status:          common.TopUpStatusSuccess,
	}
	if err = topUp.Insert(); err != nil {
		DB.Model(&X402Hold{}).Where("id = ?", h.Id).Update("status", X402HoldStatusHeld)
		return false, err
	}
	quota := int(decimal.NewFromInt(h.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
	if err = IncreaseUserQuota(userId, quota, true); err != nil {
		DB.Model(&X402Hold{}).Where("id = ?", h.Id).Update("status", X402HoldStatusHeld)
		return false, err
	}
	RecordTopupLog(userId,
		fmt.Sprintf("微信AI支付(X402)充值成功，充值金额: %v，支付金额：%f", logger.FormatQuota(quota), h.Money),
		callerIp, PaymentMethodWechat, PaymentMethodWechat)
	h.Status = X402HoldStatusCredited
	h.ClaimedAt = now
	h.ClaimedBy = userId
	return true, nil
}
