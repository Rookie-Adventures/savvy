package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// AgentRefundRequest 智能体代提交的退款申请工单。
// 产品形态：资金变动仍由人工处理（退款要走渠道原路退回，不能让智能体直接触发），
// 这里只负责把用户的诉求落成一条可查、可派单的记录——用户能凭 ticket_no 追问，
// 管理员能按 pending 列表处理。不碰 TopUp.Status，避免和支付回调状态机打架。
type AgentRefundRequest struct {
	Id            int     `json:"id" gorm:"primaryKey;autoIncrement"`
	TicketNo      string  `json:"ticket_no" gorm:"type:varchar(32);unique;index"`
	UserId        int     `json:"user_id" gorm:"index"`
	TradeNo       string  `json:"trade_no" gorm:"type:varchar(64);index"`
	AmountYuan    float64 `json:"amount_yuan"`
	Reason        string  `json:"reason" gorm:"type:varchar(512)"`
	Status        string  `json:"status" gorm:"type:varchar(20);index;default:'pending'"`
	Source        string  `json:"source" gorm:"type:varchar(20)"` // agent(智能体)/web(将来前端复用)
	CreateTime    int64   `json:"create_time"`
	HandleTime    int64   `json:"handle_time"`
	HandleAdminId int     `json:"handle_admin_id"`
	AdminRemark   string  `json:"admin_remark" gorm:"type:varchar(512)"`
}

const (
	AgentRefundStatusPending  = "pending"
	AgentRefundStatusApproved = "approved"
	AgentRefundStatusRejected = "rejected"

	AgentRefundSourceAgent = "agent"

	AgentRefundMaxReasonLen = 200
)

var (
	// ErrAgentRefundDuplicate 同一用户对同一订单已有在途工单，重复提交直接拒绝（幂等，避免刷单）
	ErrAgentRefundDuplicate = errors.New("该订单已有在途的退款申请")
	ErrAgentRefundNotFound  = errors.New("退款申请不存在")
)

func IsValidAgentRefundStatus(s string) bool {
	switch s {
	case AgentRefundStatusPending, AgentRefundStatusApproved, AgentRefundStatusRejected:
		return true
	}
	return false
}

// NewAgentRefundTicketNo RFD + yyyymmdd + 8 位随机 = 20 位，够短能口头报给用户
func NewAgentRefundTicketNo() string {
	return fmt.Sprintf("RFD%s%s", time.Now().Format("20060102"), common.GetRandomString(8))
}

// CreateAgentRefundRequest 落单前做在途去重（同用户同订单只留一条 pending）
func CreateAgentRefundRequest(r *AgentRefundRequest) error {
	r.TradeNo = strings.TrimSpace(r.TradeNo)
	r.Reason = strings.TrimSpace(r.Reason)
	r.TicketNo = strings.TrimSpace(r.TicketNo)
	if r.Status == "" {
		r.Status = AgentRefundStatusPending
	}
	if r.CreateTime == 0 {
		r.CreateTime = time.Now().Unix()
	}
	var dup int64
	if err := DB.Model(&AgentRefundRequest{}).
		Where("user_id = ? AND trade_no = ? AND status = ?", r.UserId, r.TradeNo, AgentRefundStatusPending).
		Count(&dup).Error; err != nil {
		return err
	}
	if dup > 0 {
		return ErrAgentRefundDuplicate
	}
	return DB.Create(r).Error
}

func GetAgentRefundByTicket(ticketNo string) (*AgentRefundRequest, error) {
	r := &AgentRefundRequest{}
	err := DB.Where("ticket_no = ?", strings.TrimSpace(ticketNo)).First(r).Error
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetAgentRefundRequestsByUser 用户自己的工单列表（新→旧）
func GetAgentRefundRequestsByUser(userId int, pageInfo *common.PageInfo) ([]*AgentRefundRequest, int64, error) {
	var (
		rows  []*AgentRefundRequest
		total int64
	)
	if err := DB.Model(&AgentRefundRequest{}).Where("user_id = ?", userId).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := DB.Where("user_id = ?", userId).
		Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&rows).Error
	return rows, total, err
}

// GetAgentRefundRequestsByStatus 管理员按状态捞待处理队列；status 为空则全量
func GetAgentRefundRequestsByStatus(status string, pageInfo *common.PageInfo) ([]*AgentRefundRequest, int64, error) {
	var (
		rows  []*AgentRefundRequest
		total int64
	)
	tx := DB.Model(&AgentRefundRequest{})
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q := DB.Model(&AgentRefundRequest{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("id asc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&rows).Error
	return rows, total, err
}

// HandleAgentRefundRequest 管理员处理：只允许处理 pending 单（已处理的重复点击会被拒绝）
func HandleAgentRefundRequest(id int, status string, adminId int, remark string) error {
	if !IsValidAgentRefundStatus(status) || status == AgentRefundStatusPending {
		return ErrAgentRefundNotFound
	}
	r := &AgentRefundRequest{}
	if err := DB.Where("id = ?", id).First(r).Error; err != nil {
		return err
	}
	if r.Status != AgentRefundStatusPending {
		return errors.New("退款申请已处理")
	}
	return DB.Model(&AgentRefundRequest{}).Where("id = ? AND status = ?", id, AgentRefundStatusPending).
		Updates(map[string]interface{}{
			"status":          status,
			"handle_time":     time.Now().Unix(),
			"handle_admin_id": adminId,
			"admin_remark":    strings.TrimSpace(remark),
		}).Error
}
