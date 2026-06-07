package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// consolidateMemoriesTool runs maintenance on the memory engine.
type consolidateMemoriesTool struct {
	cfg    Config
	logger *logger.Logger
}

func newConsolidateMemoriesTool(cfg Config) *consolidateMemoriesTool {
	ensureSandbox(&cfg)
	return &consolidateMemoriesTool{cfg: cfg, logger: cfg.Logger}
}

func (consolidateMemoriesTool) Name() string { return "ConsolidateMemories" }

func (consolidateMemoriesTool) Description() string {
	return "Run memory maintenance: apply confidence decay to knowledge graph edges, " +
		"detect stale memories with low salience, and find entity communities. " +
		"Use this periodically to keep the memory system healthy."
}

func (consolidateMemoriesTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{},
  "required":[]
}`)
}

func (t *consolidateMemoriesTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryEngine == nil {
		return "", fmt.Errorf("memory engine is not configured; check your settings")
	}

	report, err := t.cfg.MemoryEngine.Consolidate(ctx)
	if err != nil {
		return "", fmt.Errorf("consolidation failed: %w", err)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "consolidate: completed",
			"edges_decayed", report.EdgesDecayed,
			"stale_removed", report.StaleMemoriesRemoved,
			"communities", report.CommunitiesFound,
		)
	}

	b, _ := json.Marshal(report)
	return string(b), nil
}

var _ Tool = (*consolidateMemoriesTool)(nil)
