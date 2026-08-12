package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
)

// recallEntityTool traverses the knowledge graph from an entity.
type recallEntityTool struct {
	cfg    Config
	logger *logger.Logger
}

func newRecallEntityTool(cfg Config) *recallEntityTool {
	ensureExecutor(&cfg)
	return &recallEntityTool{cfg: cfg, logger: cfg.Logger}
}

func (recallEntityTool) Name() string { return "RecallEntity" }

func (recallEntityTool) Description() string {
	return "Traverse the knowledge graph from an entity to find related memories. " +
		"Use this when you want to explore what the system knows about a specific entity " +
		"or find all memories connected to a person, project, or concept."
}

func (recallEntityTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "entity":{"type":"string","description":"The entity name to start from."},
    "max_hops":{"type":"integer","description":"Maximum traversal depth. Default 2."},
    "limit":{"type":"integer","description":"Maximum results. Default 10."}
  },
  "required":["entity"]
}`)
}

type recallEntityArgs struct {
	Entity  string `json:"entity"`
	MaxHops int    `json:"max_hops,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

func (t *recallEntityTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryAccess == nil {
		return "", fmt.Errorf("memory_not_configured")
	}

	var a recallEntityArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("entity", a.Entity); err != nil {
		return "", err
	}

	if a.MaxHops <= 0 {
		a.MaxHops = 2
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.MaxHops > 4 {
		a.MaxHops = 4
	}
	if a.Limit > 20 {
		a.Limit = 20
	}

	results, err := t.cfg.MemoryAccess.RecallEntity(ctx, a.Entity, a.MaxHops, a.Limit)
	if err != nil {
		return "", memoryToolError(ctx, err)
	}

	result, err := json.Marshal(struct {
		Untrusted bool                  `json:"untrusted"`
		Results   []engine.SearchResult `json:"results"`
	}{Untrusted: true, Results: results})
	if err != nil {
		return "", fmt.Errorf("memory_unavailable")
	}
	return string(result), nil
}

var _ Tool = (*recallEntityTool)(nil)
