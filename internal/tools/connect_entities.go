package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// connectEntitiesTool finds the shortest path between two entities in the KG.
type connectEntitiesTool struct {
	cfg    Config
	logger *logger.Logger
}

func newConnectEntitiesTool(cfg Config) *connectEntitiesTool {
	ensureSandbox(&cfg)
	return &connectEntitiesTool{cfg: cfg, logger: cfg.Logger}
}

func (connectEntitiesTool) Name() string { return "ConnectEntities" }

func (connectEntitiesTool) Description() string {
	return "Find the shortest path between two entities in the knowledge graph. " +
		"Use this to discover how two concepts, people, or projects are connected " +
		"through intermediate entities and relationships."
}

func (connectEntitiesTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "source":{"type":"string","description":"The starting entity name."},
    "target":{"type":"string","description":"The target entity name."},
    "max_depth":{"type":"integer","description":"Maximum path depth. Default 5."}
  },
  "required":["source","target"]
}`)
}

type connectEntitiesArgs struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

func (t *connectEntitiesTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryEngine == nil {
		return "", fmt.Errorf("memory engine is not configured; check your settings")
	}

	var a connectEntitiesArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("source", a.Source); err != nil {
		return "", err
	}
	if err := validateNotZeroLen("target", a.Target); err != nil {
		return "", err
	}

	if a.MaxDepth <= 0 {
		a.MaxDepth = 5
	}

	nodes, edges, err := t.cfg.MemoryEngine.ShortestPath(ctx, a.Source, a.Target, a.MaxDepth)
	if err != nil {
		return "", fmt.Errorf("no path found: %w", err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Path from %q to %q:\n\n", a.Source, a.Target))
	if len(nodes) > 0 {
		b.WriteString("Entities: ")
		names := make([]string, len(nodes))
		for i, n := range nodes {
			names[i] = n.Name
		}
		b.WriteString(strings.Join(names, " → "))
		b.WriteString("\n\n")
	}
	for i, e := range edges {
		b.WriteString(fmt.Sprintf("%d. %s --[%s]--> %s (weight: %.2f)\n",
			i+1, e.SourceName, e.RelType, e.TargetName, e.Weight))
	}
	return b.String(), nil
}

var _ Tool = (*connectEntitiesTool)(nil)
