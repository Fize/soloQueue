package skill

import (
	"fmt"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Skill 接口 ────────────────────────────────────────────────────────────

// SkillCategory 区分内置与外置 Skill
type SkillCategory string

const (
	// SkillBuiltin 内置 Skill（Go 代码注册）
	SkillBuiltin SkillCategory = "builtin"
	// SkillUser 用户外置 Skill（.soloqueue/skills/ 目录，后续 Phase）
	SkillUser SkillCategory = "user"
)

// Skill 是 Agent 能力的组织单元
//
// Skill 是独立于 Tool 的概念：
//   - BuiltinSkill 内部包含 Tool 实例（通过 toolProvider 接口暴露给 SkillRegistry）
//   - UserSkill 可能完全不含 Tool（如 prompt 注入型能力）
//   - Skill 接口本身不依赖 Tool
type Skill interface {
	// ID 返回 Skill 唯一标识（如 "fs", "web", "delegate"）
	ID() string

	// Description 给 LLM 看的自然语言描述
	Description() string

	// Category 返回 Skill 分类
	Category() SkillCategory
}

// toolProvider 是包内部接口，用于从 Skill 中提取 Tool
//
// BuiltinSkill 和 DelegateSkill 实现此接口，但不对包外暴露。
// SkillRegistry.Register 时通过类型断言检查。
type toolProvider interface {
	provideTools() []tools.Tool
}

// ─── BuiltinSkill ──────────────────────────────────────────────────────────

// BuiltinSkill 内置 Skill 的标准实现
type BuiltinSkill struct {
	id          string
	description string
	tools       []tools.Tool
}

// NewBuiltinSkill 构造内置 Skill
//
// id 不能为空（注册时报错）。
// tools 可以为空（某些内置 Skill 可能只提供描述性元数据）。
func NewBuiltinSkill(id, description string, ts ...tools.Tool) *BuiltinSkill {
	return &BuiltinSkill{id: id, description: description, tools: ts}
}

func (s *BuiltinSkill) ID() string             { return s.id }
func (s *BuiltinSkill) Description() string    { return s.description }
func (s *BuiltinSkill) Category() SkillCategory { return SkillBuiltin }

// Tools 返回该 Skill 包含的 Tool 列表（公开方法，供外部查询）
func (s *BuiltinSkill) Tools() []tools.Tool { return s.tools }

// provideTools 实现 toolProvider 内部接口
func (s *BuiltinSkill) provideTools() []tools.Tool { return s.tools }

// ─── SkillRegistry ─────────────────────────────────────────────────────────

// Skill 相关错误
var (
	// ErrSkillIDEmpty Register 时 Skill.ID() 为空
	ErrSkillIDEmpty = fmt.Errorf("skill: skill id is empty")
	// ErrSkillAlreadyRegistered 同名 Skill 重复注册
	ErrSkillAlreadyRegistered = fmt.Errorf("skill: skill already registered")
	// ErrSkillNil Register(nil)
	ErrSkillNil = fmt.Errorf("skill: skill is nil")
)

// SkillRegistry 管理 Skill 的注册与查找
//
// 组合 ToolRegistry —— 当 Skill 实现了 toolProvider 接口时，
// 其 Tool 自动注册到底层 ToolRegistry。
// 对外暴露：
//   - Skill 维度：按 Skill ID 查找/列举
//   - Tool 维度：复用 ToolRegistry 的 Get/Specs/Len/Names
type SkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]Skill        // skillID → Skill
	tools  *tools.ToolRegistry     // 组合，管理所有 Tool
}

// NewSkillRegistry 构造空 SkillRegistry
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills: make(map[string]Skill),
		tools:  tools.NewToolRegistry(),
	}
}

// Register 注册一个 Skill
//
// 若 Skill 实现了 toolProvider（如 BuiltinSkill、DelegateSkill），
// 其 Tool 会自动注册到底层 ToolRegistry。
//
// 错误：
//   - s == nil          → ErrSkillNil
//   - s.ID() == ""      → ErrSkillIDEmpty
//   - 同名已注册        → ErrSkillAlreadyRegistered
//   - Tool 重名/nil 等  → 透传 ToolRegistry.Register 错误
func (r *SkillRegistry) Register(s Skill) error {
	if s == nil {
		return ErrSkillNil
	}
	id := s.ID()
	if id == "" {
		return ErrSkillIDEmpty
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.skills[id]; ok {
		return fmt.Errorf("%w: %s", ErrSkillAlreadyRegistered, id)
	}

	// 若 Skill 实现了 toolProvider，提取并注册 Tool
	if tp, ok := s.(toolProvider); ok {
		toolList := tp.provideTools()
		for _, t := range toolList {
			if err := r.tools.Register(t); err != nil {
				// 回滚已注册的 Tool
				for _, rt := range toolList {
					if rt == t {
						break
					}
					r.tools.Unregister(rt.Name())
				}
				return fmt.Errorf("skill %q tool register: %w", id, err)
			}
		}
	}

	r.skills[id] = s
	return nil
}

// GetSkill 按 Skill ID 查找
func (r *SkillRegistry) GetSkill(id string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[id]
	return s, ok
}

// Skills 返回所有已注册 Skill 的快照
func (r *SkillRegistry) Skills() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	return out
}

// ToolSpecs 返回所有 Skill 暴露的 Tool 声明（给 LLM 用）
// 等价于底层 ToolRegistry.Specs()
func (r *SkillRegistry) ToolSpecs() []llm.ToolDef {
	return r.tools.Specs()
}

// ToolRegistry 返回底层 ToolRegistry（供 Agent 执行 tool 时查找）
func (r *SkillRegistry) ToolRegistry() *tools.ToolRegistry {
	return r.tools
}

// Len 当前 Skill 数量
func (r *SkillRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}
