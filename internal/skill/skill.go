// Package skill 实现可执行的技能系统
//
// Skill 是 Agent 可调用的技能定义，LLM 通过 Skill 内置工具激活。
// 重构后的设计对齐 Claude Code 的 Skill 机制：
//
//   - Skill 是不可变的数据定义（导出字段 struct，非接口）
//   - SkillTool 实现 tools.Tool，LLM 通过 function calling 调用
//   - 支持 inline（指令注入当前对话）和 fork（隔离子 agent）两种执行模式
//   - 支持 allowed-tools 白名单、$ARGUMENTS 替换、!`command` shell 执行、@file 引用
//
// 依赖方向：
//
//	tools 不依赖任何人（定义 Tool 接口）
//	skill → tools（SkillTool 实现 Tool 接口）
//	agent → skill + tools（同时持有两个 Registry）
package skill

import (
	"fmt"
	"sort"
	"sync"
)

// ─── Skill 类型 ────────────────────────────────────────────────────────────

// SkillCategory 区分内置与外置 Skill
type SkillCategory string

const (
	// SkillBuiltin 内置 Skill（Go 代码注册）
	SkillBuiltin SkillCategory = "builtin"
	// SkillUser 用户外置 Skill（SKILL.md 文件加载）
	SkillUser SkillCategory = "user"
)

// Skill 是一个不可变的技能定义（构造后不修改）
//
// 对齐 Claude Code 的 Skill 机制：Skill 是 LLM 可调用的可执行技能，
// 包含指令内容、工具白名单、执行模式等配置。
// LLM 通过 Skill 内置工具激活 skill，而非手动 Read SKILL.md。
type Skill struct {
	// ID 唯一标识（如 "commit", "deploy"），必须非空
	ID string

	// Description 给 LLM 看的自然语言描述（简要说明）
	Description string

	// WhenToUse 额外触发条件描述，附加到 Description 后供 LLM 判断何时调用
	WhenToUse string

	// Instructions 完整指令内容
	//
	// 对于从 SKILL.md 加载的 skill，这是 body 部分；
	// 对于内置 skill，这是 Go 代码中定义的指令文本。
	Instructions string

	// AllowedTools 工具白名单；nil 表示不限制
	//
	// 支持模式：Bash(git:*), Edit(src/**/*.ts), mcp__server__tool
	AllowedTools []string

	// DisableModelInvocation 为 true 时不出现在 Skill tool description 中
	// 只能通过 /skill-name 斜杠命令手动触发
	DisableModelInvocation bool

	// UserInvocable 为 false 时不出现在 / 菜单中
	// 仅供 AI 内部调用或其他 skill 引用
	UserInvocable bool

	// Context 执行模式："fork" 表示隔离子 agent，"" 表示 inline
	Context string

	// Agent fork 时的子 agent 类型（如 "general-purpose", "Explore"）
	Agent string

	// Category 返回 Skill 分类（builtin 或 user）
	Category SkillCategory

	// FilePath SKILL.md 的绝对路径（内置 skill 为空）
	FilePath string

	// Dir SKILL.md 所在目录（支持文件引用解析）
	Dir string
}

// ─── 构造函数 ──────────────────────────────────────────────────────────────

// SkillOption 是 Skill 的可选配置
type SkillOption func(*Skill)

// WithAllowedTools 设置工具白名单
func WithAllowedTools(tools []string) SkillOption {
	return func(s *Skill) { s.AllowedTools = tools }
}

// WithDisableModelInvocation 禁止 AI 自动调用
func WithDisableModelInvocation() SkillOption {
	return func(s *Skill) { s.DisableModelInvocation = true }
}

// WithUserInvocable 设置是否出现在 / 菜单
func WithUserInvocable(v bool) SkillOption {
	return func(s *Skill) { s.UserInvocable = v }
}

// WithContext 设置执行模式（"fork" 或 ""）
func WithContext(ctx string) SkillOption {
	return func(s *Skill) { s.Context = ctx }
}

// WithAgent 设置 fork 时的子 agent 类型
func WithAgent(agent string) SkillOption {
	return func(s *Skill) { s.Agent = agent }
}

// NewBuiltinSkill 构造内置 Skill
//
// id 不能为空（注册时报错）。
// instructions 为 skill 的完整指令。
func NewBuiltinSkill(id, desc, instructions string, opts ...SkillOption) *Skill {
	s := &Skill{
		ID:            id,
		Description:   desc,
		Instructions:  instructions,
		UserInvocable: true,
		Category:      SkillBuiltin,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// maxSkillDescriptionChars 是 combined description 的最大字符数，对齐 Claude Code
const maxSkillDescriptionChars = 1536

// CombinedDescription 合并 Description 和 WhenToUse，截断至 maxSkillDescriptionChars
//
// 对齐 Claude Code 行为：
//   - when_to_use 追加到 description 后
//   - 合并文本上限 1536 字符
//   - 优先保留开头内容（关键用例放在 description 前面）
func (s *Skill) CombinedDescription() string {
	if s.WhenToUse == "" {
		return s.Description
	}
	combined := s.Description + "\n" + s.WhenToUse
	if len(combined) <= maxSkillDescriptionChars {
		return combined
	}
	// 截断：优先保留 description 完整，剩余配额给 when_to_use 的开头部分
	descLen := len(s.Description)
	if descLen >= maxSkillDescriptionChars {
		return s.Description[:maxSkillDescriptionChars]
	}
	whenToUseBudget := maxSkillDescriptionChars - descLen - 1 // 1 for '\n'
	if whenToUseBudget <= 0 {
		return s.Description
	}
	return s.Description + "\n" + s.WhenToUse[:whenToUseBudget]
}

// ─── SkillRegistry ─────────────────────────────────────────────────────────

// Skill 相关错误
var (
	// ErrSkillIDEmpty Register 时 Skill.ID 为空
	ErrSkillIDEmpty = fmt.Errorf("skill: skill id is empty")
	// ErrSkillAlreadyRegistered 同名 Skill 重复注册
	ErrSkillAlreadyRegistered = fmt.Errorf("skill: skill already registered")
	// ErrSkillNil Register(nil)
	ErrSkillNil = fmt.Errorf("skill: skill is nil")
)

// SkillRegistry 管理 Skill 的注册和查找
type SkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]*Skill // skillID → Skill
}

// NewSkillRegistry 构造空 SkillRegistry
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills: make(map[string]*Skill),
	}
}

// Register 注册一个 Skill
//
// 错误：
//   - s == nil      → ErrSkillNil
//   - s.ID == ""    → ErrSkillIDEmpty
//   - 同名已注册    → ErrSkillAlreadyRegistered
func (r *SkillRegistry) Register(s *Skill) error {
	if s == nil {
		return ErrSkillNil
	}
	if s.ID == "" {
		return ErrSkillIDEmpty
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.skills[s.ID]; ok {
		return fmt.Errorf("%w: %s", ErrSkillAlreadyRegistered, s.ID)
	}

	r.skills[s.ID] = s
	return nil
}

// GetSkill 按 ID 查找
func (r *SkillRegistry) GetSkill(id string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[id]
	return s, ok
}

// Skills 返回所有已注册 Skill 的快照（按 ID 字典序）
func (r *SkillRegistry) Skills() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// Rebuild clears the registry and reloads all skills from the given directories.
func (r *SkillRegistry) Rebuild(dirs map[string]string) error {
	userSkills, err := LoadSkillsFromDirs(dirs)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.skills = make(map[string]*Skill, len(userSkills))
	for _, s := range userSkills {
		r.skills[s.ID] = s
	}
	r.mu.Unlock()
	return nil
}

// Len 当前 Skill 数量
func (r *SkillRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}
