package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── SkillTool ─────────────────────────────────────────────────────────────

// SkillTool 让 LLM 通过 function calling 调用 Skill
//
// 对齐 Claude Code 的 Skill 机制：LLM 调用 Skill(skill="commit", args="...")
// 时，SkillTool 从 SkillRegistry 查找 skill，执行预处理管道，
// 然后根据 skill.Context 决定 inline 还是 fork 执行。
//
// SkillTool 的 Description 动态编译所有非 disable-model-invocation 的 skill 列表，
// 让 LLM 知道何时该用哪个 skill。
type SkillTool struct {
	registry  *SkillRegistry
	forkSpawn SkillForkSpawnFn // nil 时 fork 模式降级为 inline
	logger    *logger.Logger
}

// SkillToolOption 是 SkillTool 的可选配置
type SkillToolOption func(*SkillTool)

// WithSkillLogger 设置 SkillTool 的日志实例
func WithSkillLogger(l *logger.Logger) SkillToolOption {
	return func(st *SkillTool) { st.logger = l }
}

// NewSkillTool 构造 SkillTool
//
// registry 不能为 nil。forkSpawn 可以为 nil（此时 fork 模式降级为 inline）。
func NewSkillTool(registry *SkillRegistry, forkSpawn SkillForkSpawnFn, opts ...SkillToolOption) *SkillTool {
	st := &SkillTool{
		registry:  registry,
		forkSpawn: forkSpawn,
	}
	for _, opt := range opts {
		opt(st)
	}
	return st
}

// skillToolArgs 是 SkillTool 的参数结构
type skillToolArgs struct {
	Skill string `json:"skill"`
	Args  string `json:"args,omitempty"`
}

func (SkillTool) Name() string { return "Skill" }

// Description 动态生成 skill 列表供 LLM 判断何时使用
func (t *SkillTool) Description() string {
	skills := t.registry.Skills()
	if len(skills) == 0 {
		return "Invoke a skill by name. No skills are currently available."
	}

	var b strings.Builder
	b.WriteString("Invoke a skill by name. Available skills:\n")
	for _, s := range skills {
		if s.DisableModelInvocation {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s", s.ID, s.Description)
		if s.Context == "fork" {
			b.WriteString(" [fork]")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (SkillTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"skill": {"type": "string", "description": "Name of the skill to invoke"},
			"args":  {"type": "string", "description": "Optional arguments to pass to the skill"}
		},
		"required": ["skill"]
	}`)
}

// Execute 执行 skill 调用
//
// 流程：
//  1. 解析参数（skill name + optional args）
//  2. 从 registry 查找 skill
//  3. 执行预处理管道（$ARGUMENTS 替换等）
//  4. 根据 skill.Context 决定执行模式：
//     inline → 返回预处理后的内容，LLM 据此继续调用工具执行
//     fork   → 创建子 agent 执行，返回子 agent 结果
func (t *SkillTool) Execute(ctx context.Context, rawArgs string) (string, error) {
	var args skillToolArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return fmt.Sprintf("error: invalid skill arguments: %s", err), nil
	}

	if t.logger != nil {
		t.logger.DebugContext(ctx, logger.CatTool, "skill: executing",
			"skill_id", args.Skill, "has_args", args.Args != "")
	}

	s, ok := t.registry.GetSkill(args.Skill)
	if !ok {
		if t.logger != nil {
			t.logger.WarnContext(ctx, logger.CatTool, "skill: not found",
				"skill_id", args.Skill)
		}
		return fmt.Sprintf("error: skill %q not found", args.Skill), nil
	}

	// 预处理管道
	content := PreprocessContent(s.Instructions, args.Args, s.Dir)

	// 执行模式
	switch s.Context {
	case "fork":
		if t.forkSpawn != nil {
			result, err := ExecuteFork(ctx, s, content, args.Args, t.forkSpawn)
			if err != nil {
				if t.logger != nil {
					t.logger.WarnContext(ctx, logger.CatTool, "skill: fork failed",
						"skill_id", s.ID, "err", err.Error())
				}
				return fmt.Sprintf("error: skill %q fork execution failed: %s", s.ID, err), nil
			}
			if t.logger != nil {
				t.logger.DebugContext(ctx, logger.CatTool, "skill: fork completed",
					"skill_id", s.ID, "result_len", len(result))
			}
			return result, nil
		}
		// forkSpawn 未设置，降级为 inline
		fallthrough
	default:
		if t.logger != nil {
			t.logger.DebugContext(ctx, logger.CatTool, "skill: inline completed",
				"skill_id", s.ID, "content_len", len(content))
		}
		// inline 模式：返回预处理后的 skill content
		// LLM 将此作为 tool result 消费，然后根据 skill instructions 继续行动
		return content, nil
	}
}
