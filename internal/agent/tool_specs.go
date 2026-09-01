package agent

import "github.com/xiaobaitu/soloqueue/internal/llm"

// ToolSpecs returns the definitions of tools registered on the agent.
func (a *Agent) ToolSpecs() []llm.ToolDef {
	if a.tools == nil {
		return nil
	}
	return a.tools.Specs()
}
