package agent

import "time"

// Role 区分系统内置 agent 和用户创建 agent
type Role string

const (
	RoleSystem Role = "system"
	RoleUser   Role = "user"
)

// Kind 描述 agent 的行为类型
//
// 本 phase 仅保留 KindChat / KindCustom 作为占位；真正的行为分支
// （code / planner / evaluator 等）等到 tool 系统落地时按需扩展。
type Kind string

const (
	KindChat   Kind = "chat"
	KindCustom Kind = "custom"
)

// Definition 是 agent 的静态配置
//
// 所有字段都是"起 agent 时一次性写入"的不可变数据。
// 不含 supervision / restart policy —— 本 phase agent 不自管生命周期。
type Definition struct {
	ID           string
	Name         string
	TeamID       string
	Role         Role
	Kind         Kind
	ModelID      string
	ProviderID   string
	SystemPrompt string
	Temperature  float64
	MaxTokens    int
	CreatedAt    time.Time
}
