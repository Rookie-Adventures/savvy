package service

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// Agent 能力层：把「对用户有用的读/写能力」收敛成一层，供 SKILL.md（含百炼 HTTP 工具）调用。
// 约定：
//  1. 所有能力都要求真实用户身份（session 或 Authorization: Bearer <access_token>），
//     游客不能查余额/兑换/退款——凭据 claim_token 只在订单维度有效，不能当身份用。
//  2. money↔quota 换算统一走 agentQuotaAmountFromMoney 的同源公式（common.QuotaPerUnit + display type），
//     避免 "钱包里显示 5 刀、这里报 2500000" 这类双份真值。
//  3. 写操作（兑换/退款申请）走 controller 层的 CriticalRateLimit，本文件只管业务。

const (
	agentUsageMaxDays = 30
	agentUsageMinDays = 1
	// 运营类：低于这个余额就由 SKILL 主动引导续费（单位=额度展示单位，见 QuotaPerUnit 换算）
	agentLowBalanceDefaultUnits = 2
)

// AgentBalanceView 余额视图。raw 字段给程序用，display 字段给模型直接转述给用户。
type AgentBalanceView struct {
	UserId              int     `json:"user_id"`
	Username            string  `json:"username"`
	Group               string  `json:"group"`
	Quota               int     `json:"quota"`
	UsedQuota           int     `json:"used_quota"`
	RemainingQuota      int     `json:"remaining_quota"`
	RemainingUnits      float64 `json:"remaining_units"`
	RemainingDisplay    string  `json:"remaining_display"`
	LowBalance          bool    `json:"low_balance"`
	LowBalanceUnits     float64 `json:"low_balance_units"`
	RechargeUrl         string  `json:"recharge_url"`
	SuggestedAmountYuan []int   `json:"suggested_amount_yuan"`
}

// AgentUsageView 用量视图：近 N 天总消耗 + 请求数（模型要看趋势时够用）
type AgentUsageView struct {
	Days            int    `json:"days"`
	Start           int64  `json:"start_time"`
	End             int64  `json:"end_time"`
	ConsumedQuota   int    `json:"consumed_quota"`
	ConsumedDisplay string `json:"consumed_display"`
	RequestCount    int64  `json:"request_count"`
}

// AgentTopUpView 最近充值订单（脱敏：不带选择限制 expose claim_token）
type AgentTopUpView struct {
	TradeNo       string  `json:"trade_no"`
	Money         float64 `json:"money"`
	Amount        int64   `json:"amount"`
	Status        string  `json:"status"`
	PaymentMethod string  `json:"payment_method"`
	CreateTime    int64   `json:"create_time"`
	CompleteTime  int64   `json:"complete_time"`
}

// quotaToUnits 额度 → 展示单位：$ / ¥ / token 由用户自身显示配置决定；
// 这里给纯数值（logger.LogQuota 那份是给人看的字符串，带符号）
func quotaToUnits(quota int) float64 {
	return float64(quota) / common.QuotaPerUnit
}

// AgentQuota.lowBalanceThresholdUnits 用户没配预警阈值时用默认 2 单位；
// 配了则用 Usersetting.QuotaWarningThreshold（前端同字段，单位一致）
func lowBalanceThresholdUnits(user *model.User) float64 {
	if v := user.GetSetting().QuotaWarningThreshold; v > 0 {
		return v
	}
	return agentLowBalanceDefaultUnits
}

func agentRechargeUrl() string {
	return strings.TrimSuffix(system_setting.ServerAddress, "/") + "/wallet"
}

// AgentQueryBalance 余额 + 是否该提醒续费（运营类能力的数据源）
func AgentQueryBalance(userId int) (*AgentBalanceView, error) {
	user, err := model.GetUserById(userId, false)
	if err != nil {
		return nil, err
	}
	quota, err := model.GetUserQuota(userId, false)
	if err != nil {
		return nil, err
	}
	used, err := model.GetUserUsedQuota(userId)
	if err != nil {
		return nil, err
	}
	remaining := quota - used
	if remaining < 0 {
		remaining = 0
	}
	threshold := lowBalanceThresholdUnits(user)
	units := quotaToUnits(remaining)
	return &AgentBalanceView{
		UserId:              userId,
		Username:            user.Username,
		Group:               user.Group,
		Quota:               quota,
		UsedQuota:           used,
		RemainingQuota:      remaining,
		RemainingUnits:      round2(units),
		RemainingDisplay:    logger.LogQuota(remaining),
		LowBalance:          units < threshold,
		LowBalanceUnits:     round2(threshold),
		RechargeUrl:         agentRechargeUrl(),
		SuggestedAmountYuan: []int{10, 50, 100},
	}, nil
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// AgentQueryUsage 近 days 天用量；超出窗口钳到 [1,30]，防止被模型传 3650 打爆日志库
func AgentQueryUsage(userId int, days int) (*AgentUsageView, error) {
	if days < agentUsageMinDays {
		days = agentUsageMinDays
	}
	if days > agentUsageMaxDays {
		days = agentUsageMaxDays
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	end := now.Unix()
	start := now.AddDate(0, 0, -days).Unix()
	stat, err := model.SumUsedQuota(model.LogTypeConsume, start, end, "", user.Username, "", 0, "")
	if err != nil {
		return nil, err
	}
	// ponytail: SumUsedQuota 的 Rpm 只看最近 60s（设计给大盘用），窗口内请求数得自己 count
	_, reqCount, cerr := model.GetUserLogs(userId, model.LogTypeConsume, start, end, "", "", 0, 1, "", "", "")
	if cerr != nil {
		return nil, cerr
	}
	return &AgentUsageView{
		Days:            days,
		Start:           start,
		End:             end,
		ConsumedQuota:   stat.Quota,
		ConsumedDisplay: logger.LogQuota(stat.Quota),
		RequestCount:    reqCount,
	}, nil
}

// AgentQueryTopUps 最近充值订单（复用用户自有的 Paginateed 查询，去掉游标外的数据）
func AgentQueryTopUps(userId int, page int, pageSize int) ([]AgentTopUpView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	pageInfo := &common.PageInfo{Page: page, PageSize: pageSize}
	rows, total, err := model.GetUserTopUps(userId, pageInfo)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AgentTopUpView, 0, len(rows))
	for _, r := range rows {
		out = append(out, AgentTopUpView{
			TradeNo:       r.TradeNo,
			Money:         r.Money,
			Amount:        r.Amount,
			Status:        r.Status,
			PaymentMethod: r.PaymentMethod,
			CreateTime:    r.CreateTime,
			CompleteTime:  r.CompleteTime,
		})
	}
	return out, total, nil
}

// AgentRedeem 优惠码兑换：直接复用 model.Redeem 的事务（幂等、过期与已用校验都在里面）
func AgentRedeem(userId int, code string) (int, error) {
	quota, err := model.Redeem(strings.TrimSpace(code), userId)
	if err != nil {
		if errors.Is(err, model.ErrRedeemFailed) {
			return 0, errors.New("兑换码无效或已被使用")
		}
		return 0, err
	}
	return quota, nil
}

// AgentRefundApplyInput 退款申请入参
type AgentRefundApplyInput struct {
	UserId     int
	TradeNo    string
	Reason     string
	AmountYuan float64
}

// AgentApplyRefund 校验订单归属（必须是本人的已支付单）后落工单。
// ponytail: AmountYuan 取自订单实收，不采信模型/用户的申报值，避免虚报金额。
func AgentApplyRefund(in AgentRefundApplyInput) (*model.AgentRefundRequest, error) {
	tradeNo := strings.TrimSpace(in.TradeNo)
	if tradeNo == "" {
		return nil, errors.New("缺少订单号")
	}
	if len(in.Reason) > model.AgentRefundMaxReasonLen {
		return nil, errors.New("退款原因过长")
	}
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil || topUp.UserId != in.UserId {
		return nil, errors.New("订单不存在或不属于当前用户")
	}
	if topUp.Status != common.TopUpStatusSuccess {
		return nil, errors.New("该订单当前不可退款")
	}
	req := &model.AgentRefundRequest{
		TicketNo:   model.NewAgentRefundTicketNo(),
		UserId:     in.UserId,
		TradeNo:    tradeNo,
		AmountYuan: topUp.Money,
		Reason:     strings.TrimSpace(in.Reason),
		Status:     model.AgentRefundStatusPending,
		Source:     model.AgentRefundSourceAgent,
	}
	if err := model.CreateAgentRefundRequest(req); err != nil {
		if errors.Is(err, model.ErrAgentRefundDuplicate) {
			return nil, err
		}
		return nil, errors.New("提交退款申请失败")
	}
	return req, nil
}

// AgentListOwnRefunds 用户自己的退款工单
func AgentListOwnRefunds(userId int, page int, pageSize int) ([]*model.AgentRefundRequest, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return model.GetAgentRefundRequestsByUser(userId, &common.PageInfo{Page: page, PageSize: pageSize})
}

// AgentAdminListRefunds 管理员待处理队列
func AgentAdminListRefunds(status string, page int, pageSize int) ([]*model.AgentRefundRequest, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if status != "" && !model.IsValidAgentRefundStatus(status) {
		return nil, 0, errors.New("非法的工单状态")
	}
	return model.GetAgentRefundRequestsByStatus(status, &common.PageInfo{Page: page, PageSize: pageSize})
}

// AgentAdminHandleRefund 管理员批准/驳回
func AgentAdminHandleRefund(id int, status string, adminId int, remark string) error {
	return model.HandleAgentRefundRequest(id, status, adminId, remark)
}
