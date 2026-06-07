package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// recallEntityTool traverses the knowledge graph from an entity.
type recallEntityTool struct {
	cfg    Config
	logger *logger.Logger
}

func newRecallEntityTool(cfg Config) *recallEntityTool {
	ensureSandbox(&cfg)
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

	if t.cfg.MemoryEngine == nil {
		return "", fmt.Errorf("memory engine is not configured; check your settings")
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

	results, err := t.cfg.MemoryEngine.RecallEntity(ctx, a.Entity, a.MaxHops, a.Limit)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("No memories found for entity %q.", a.Entity), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d memories related to %q:\n\n", len(results), a.Entity))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. [%s] (score: %.2f)\n", i+1, r.Date, r.Score))
		if r.Content != "" {
			b.WriteString(fmt.Sprintf("   %s\n\n", r.Content))
		}
	}
	return b.String(), nil
}

var _ Tool = (*recallEntityTool)(nil)
