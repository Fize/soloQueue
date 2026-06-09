package deepseek

import (
	"encoding/json"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestWireMarshalWithAllBuiltinTools(t *testing.T) {
	cfg := tools.DefaultConfig()
	allTools := tools.Build(cfg)

	specs := make([]llm.ToolDef, 0, len(allTools))
	for _, tool := range allTools {
		specs = append(specs, llm.ToolDef{
			Type: "function",
			Function: llm.FunctionDecl{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Parameters(),
			},
		})
	}

	req := agent.LLMRequest{
		Model:        "deepseek-chat",
		Messages:     []agent.LLMMessage{{Role: "user", Content: "hello"}},
		Tools:        specs,
		IncludeUsage: true,
	}

	// This is exactly what ChatStream does:
	wireReq := buildWireRequest(req, true, true)

	_, err := json.Marshal(wireReq)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
}
