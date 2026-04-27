package skill

import (
	"time"

	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── DelegateSkill ─────────────────────────────────────────────────────────

// DelegateSkill 是内置的委托 Skill
//
// 根据可用的 Team Leader 动态生成 delegate_* Tool。
// LLM 看到的就是一组普通工具（如 delegate_dev, delegate_ops），
// 但底层执行时框架负责：
//  1. 从 AgentLocator 找到目标 Agent
//  2. 向目标 Agent 投递任务
//  3. 同步等待结果
//  4. 将结果作为工具返回值注入回调用方上下文
//
// 对 LLM 而言，委托 = 调用一个工具，无需知道通信协议。
type DelegateSkill struct {
	leaders []prompt.LeaderInfo // 可用的 Team Leader 列表
	locator AgentLocator        // 查找 Agent 实例
	timeout time.Duration       // 委托默认超时
}

// NewDelegateSkill 构造委托 Skill
//
// leaders 为空时 provideTools() 返回空切片（无委托能力）。
// timeout <= 0 时使用 TaskDefaultTimeout。
func NewDelegateSkill(leaders []prompt.LeaderInfo, locator AgentLocator, timeout time.Duration) *DelegateSkill {
	if timeout <= 0 {
		timeout = TaskDefaultTimeout
	}
	return &DelegateSkill{
		leaders: leaders,
		locator: locator,
		timeout: timeout,
	}
}

func (ds *DelegateSkill) ID() string              { return "delegate" }
func (ds *DelegateSkill) Description() string     { return "Delegate tasks to team leaders" }
func (ds *DelegateSkill) Category() SkillCategory  { return SkillBuiltin }

// provideTools 实现 toolProvider 内部接口：每个 LeaderInfo → 一个 DelegateTool
func (ds *DelegateSkill) provideTools() []tools.Tool {
	var ts []tools.Tool
	for _, l := range ds.leaders {
		ts = append(ts, &DelegateTool{
			LeaderID: l.Name,
			Desc:     l.Description,
			Locator:  ds.locator,
			Timeout:  ds.timeout,
		})
	}
	return ts
}
