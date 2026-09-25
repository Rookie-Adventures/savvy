package model

import (
	"github.com/QuantumNous/new-api/common"
)

// X402AgentBind — 支付侧 agent 身份 → savvy 账号 的映射表。
//
// X402(Pay Skill)链路里 Agent 每次调用会带一个稳定的主体标识(X-Agent-User-Id)。
// 首次支付时用户还没登录,钱先进 held 队列;用户用微信打开 claim 链接完成
// 登录/注册(走现有 /api/oauth/wechat 体系)后,把当时记下的 agent 标识绑到
// 这个账号上。此后同一 agent 再付款 → 直接命中绑定 → 即时入账,不再挂账。
//
// 单独建表而不加 users 列:agent 标识是外部弱信号(Agent 自报),独立映射表
// 便于解绑与审计,也不污染 users 已有的身份列。
type X402AgentBind struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	AgentUserId string `json:"agent_user_id" gorm:"column:agent_user_id;type:varchar(64);uniqueIndex;not null"`
	UserId      int    `json:"user_id" gorm:"column:user_id;index;not null"`
	CreatedAt   int64  `json:"created_at" gorm:"column:created_at;bigint"`
}

func (X402AgentBind) TableName() string { return "x402_agent_binds" }

// GetUserByX402AgentId — 未绑定返回 nil(调用方据此决定即时入账还是挂账)。
// 账号被禁用也返回 nil:钱继续留在 held 队列,不让禁用号自动收款。
func GetUserByX402AgentId(agentUserId string) *User {
	if agentUserId == "" {
		return nil
	}
	var bind X402AgentBind
	if DB.Where("agent_user_id = ?", agentUserId).First(&bind).Error != nil {
		return nil
	}
	user := &User{}
	if DB.First(user, bind.UserId).Error != nil || user.Id == 0 {
		return nil
	}
	if user.Status != common.UserStatusEnabled {
		return nil
	}
	return user
}

// BindX402AgentId — 幂等绑定。已被其他账号占用时不覆盖:资金归属只认 claim
// 链接背后的真实微信身份,避免共享设备/换标识把别人的钱挪走。
func BindX402AgentId(agentUserId string, userId int) error {
	if agentUserId == "" || userId <= 0 {
		return nil
	}
	var count int64
	if err := DB.Model(&X402AgentBind{}).Where("agent_user_id = ?", agentUserId).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return DB.Create(&X402AgentBind{
		AgentUserId: agentUserId,
		UserId:      userId,
		CreatedAt:   common.GetTimestamp(),
	}).Error
}

// ListX402AgentBindsByUser — 该账号已绑定的 agent 身份,前端用于「换个设备付款
// 也能自动入账」的说明与解绑管理。
func ListX402AgentBindsByUser(userId int) ([]X402AgentBind, error) {
	var list []X402AgentBind
	err := DB.Where("user_id = ?", userId).Order("id DESC").Limit(50).Find(&list).Error
	return list, err
}
