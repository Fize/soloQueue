// Package skill 实现上下文加载机制
//
// Skill 是独立于 Tool 的上下文注入器：
//   - Skill 决定"何时、以什么上下文引导 LLM"
//   - Tool 决定"实际做什么"
//   - Skill 不包含 Tool，不干预工具确认策略
//
// 两阶段注入：
//   1. 目录阶段：Catalog() 返回 skill 列表（ID + Description + WhenToUse + FilePath）
//      注入 system prompt，LLM 据此判断何时使用哪个 skill
//   2. 按需阶段：LLM 用 Read 工具读取 SKILL.md 全文获取完整指令
//
// 依赖方向：
//
//	tools 不依赖任何人（定义 Tool 接口）
//	skill 不依赖 tools（纯上下文，不关心执行）
//	agent → skill + tools（同时持有两个 Registry）
package skill

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ─── Skill 接口 ────────────────────────────────────────────────────────────

// SkillCategory 区分内置与外置 Skill
type SkillCategory string

const (
	// SkillBuiltin 内置 Skill（Go 代码注册）
	SkillBuiltin SkillCategory = "builtin"
	// SkillUser 用户外置 Skill（SKILL.md 文件加载）
	SkillUser SkillCategory = "user"
)

// Skill 是上下文加载机制
//
// 与 Tool 完全解耦：Skill 决定"何时、以什么上下文引导 LLM"，
// Tool 决定"实际做什么"。Skill 不包含 Tool，不干预工具确认策略。
type Skill interface {
	// ID 返回 Skill 唯一标识（如 "commit", "deploy"）
	ID() string

	// Description 给 LLM 看的自然语言描述
	//
	// 在 catalog 中紧跟 skill ID 展示，帮助 LLM 理解 skill 用途。
	Description() string

	// Instructions 返回 skill 的完整指令内容
	//
	// 对于 MDSkill，这是 SKILL.md 的 body 部分，LLM 通过 Read
	// 读取 SKILL.md 时获取；对于 BuiltinSkill，这是短指令文本。
	// 在 catalog 阶段不注入此内容，仅在按需阶段使用。
	Instructions() string

	// WhenToUse 描述触发条件（如 "用户提到部署"、"提到 commit"）
	//
	// 在 catalog 中以 "Use when:" 前缀展示，帮助 LLM 判断何时激活。
	WhenToUse() string

	// Category 返回 Skill 分类
	Category() SkillCategory

	// FilePath 返回 SKILL.md 的绝对路径
	//
	// MDSkill 返回文件路径（LLM 可用 Read 读取）。
	// BuiltinSkill 返回空字符串（指令在内存中，无文件）。
	FilePath() string
}

// ─── BuiltinSkill ──────────────────────────────────────────────────────────

// BuiltinSkill 内置 Skill 的标准实现（纯上下文，不含 Tool）
type BuiltinSkill struct {
	id           string
	description  string
	instructions string
	whenToUse    string
}

// BuiltinSkillOption 是 BuiltinSkill 的可选配置
type BuiltinSkillOption func(*BuiltinSkill)

// WithWhenToUse 设置触发条件描述
func WithWhenToUse(w string) BuiltinSkillOption {
	return func(s *BuiltinSkill) { s.whenToUse = w }
}

// NewBuiltinSkill 构造内置 Skill
//
// id 不能为空（注册时报错）。
// instructions 为 skill 的完整指令（BuiltinSkill 通常较短，目录中可直接展示）。
func NewBuiltinSkill(id, description, instructions string, opts ...BuiltinSkillOption) *BuiltinSkill {
	s := &BuiltinSkill{
		id:           id,
		description:  description,
		instructions: instructions,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *BuiltinSkill) ID() string             { return s.id }
func (s *BuiltinSkill) Description() string    { return s.description }
func (s *BuiltinSkill) Instructions() string   { return s.instructions }
func (s *BuiltinSkill) WhenToUse() string      { return s.whenToUse }
func (s *BuiltinSkill) Category() SkillCategory { return SkillBuiltin }
func (s *BuiltinSkill) FilePath() string       { return "" }

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

// catalogMaxLen 是 Catalog() 输出的最大字符数
const catalogMaxLen = 2000

// SkillRegistry 管理 Skill 的注册、查找和目录组装
//
// 与 ToolRegistry 完全独立。SkillRegistry 只管理上下文注入逻辑，
// 不持有任何 Tool。
type SkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]Skill // skillID → Skill
}

// NewSkillRegistry 构造空 SkillRegistry
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills: make(map[string]Skill),
	}
}

// Register 注册一个 Skill
//
// 错误：
//   - s == nil      → ErrSkillNil
//   - s.ID() == ""  → ErrSkillIDEmpty
//   - 同名已注册    → ErrSkillAlreadyRegistered
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

// Skills 返回所有已注册 Skill 的快照（按 ID 字典序）
func (r *SkillRegistry) Skills() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID() < out[j].ID()
	})
	return out
}

// Len 当前 Skill 数量
func (r *SkillRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}

// Catalog 返回所有 skill 的目录文本，用于注入 system prompt
//
// 格式：
//
//	## Available Skills
//
//	- **id**: description. Use when: when_to_use
//	  → `file_path`
//
// 按 skill ID 字典序排列。总字符数限制 catalogMaxLen，超出截断。
// 无 skill 时返回空字符串。
func (r *SkillRegistry) Catalog() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.skills) == 0 {
		return ""
	}

	// 按 ID 排序
	names := make([]string, 0, len(r.skills))
	for id := range r.skills {
		names = append(names, id)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("## Available Skills\n\n")

	for _, id := range names {
		s := r.skills[id]

		// - **id**
		fmt.Fprintf(&b, "- **%s**", id)

		// : description（description 可为空，靠名字自解释）
		if desc := s.Description(); desc != "" {
			fmt.Fprintf(&b, ": %s", desc)
		}

		// Use when: when_to_use（可为空）
		if w := s.WhenToUse(); w != "" {
			fmt.Fprintf(&b, ". Use when: %s", w)
		}
		b.WriteByte('\n')

		// → `file_path` (仅 MDSkill)
		if fp := s.FilePath(); fp != "" {
			fmt.Fprintf(&b, "  → `%s`\n", fp)
		}
	}

	b.WriteString("\nUse `Read` to load a skill's full instructions when needed.\n")

	result := b.String()
	if len(result) > catalogMaxLen {
		result = result[:catalogMaxLen] + "\n..."
	}
	return result
}
