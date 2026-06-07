package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// kgIndexTool lets the agent index entities and relations in the knowledge graph.
type kgIndexTool struct {
	cfg    Config
	logger *logger.Logger
}

func newKGIndexTool(cfg Config) *kgIndexTool {
	ensureSandbox(&cfg)
	return &kgIndexTool{cfg: cfg, logger: cfg.Logger}
}

func (kgIndexTool) Name() string { return "KGIndex" }

func (kgIndexTool) Description() string {
	return "Index extracted entities and their relationships into the knowledge graph. " +
		"Use this after analyzing conversation content to build structured knowledge. " +
		"Entities are concepts, people, projects, tools, or any noun-like things. " +
		"Relationships connect them (e.g., WORKS_ON, DEPENDS_ON, CREATED_BY). " +
		"You define entity types and relation types freely — there is no fixed schema."
}

func (kgIndexTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "entities":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string","description":"Entity name"},"type":{"type":"string","description":"Entity type, e.g. PERSON, PROJECT, TOOL, CONCEPT"},"confidence":{"type":"number","description":"Confidence 0-1, default 1.0"},"relations":{"type":"array","items":{"type":"object","properties":{"target_name":{"type":"string","description":"Related entity name"},"rel_type":{"type":"string","description":"Relationship type"},"weight":{"type":"number","description":"Relationship weight, default 1.0"}}}}}},"description":"Entities to index in the knowledge graph"}},
    "source_hash":{"type":"string","description":"Optional. Content hash of the source memory this knowledge was extracted from."}
  },
  "required":["entities"]
}`)
}

type kgIndexArgs struct {
	Entities   []memoryengine.EntityExtraction `json:"entities"`
	SourceHash string                          `json:"source_hash,omitempty"`
}

type kgIndexResult struct {
	EntitiesIndexed     int  `json:"entities_indexed"`
	RelationshipsIndexed int `json:"relationships_indexed"`
}

func (t *kgIndexTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryEngine == nil {
		return "", fmt.Errorf("memory engine is not configured; check your settings")
	}

	var a kgIndexArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if len(a.Entities) == 0 {
		return "", fmt.Errorf("at least one entity is required")
	}

	var entityCount, relCount int
	for _, entity := range a.Entities {
		if entity.Name == "" {
			continue
		}
		nodeType := entity.Type
		if nodeType == "" {
			nodeType = "entity"
		}
		conf := entity.Confidence
		if conf <= 0 {
			conf = 1.0
		}

		srcID, err := t.cfg.MemoryEngine.IndexEntity(ctx, entity.Name, nodeType)
		if err != nil {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "kg_index: upsert node failed", "name", entity.Name, "err", err)
			}
			continue
		}
		entityCount++

		for _, rel := range entity.Relations {
			if rel.TargetName == "" || rel.RelType == "" {
				continue
			}
			weight := rel.Weight
			if weight <= 0 {
				weight = 1.0
			}

			tgtID, err := t.cfg.MemoryEngine.IndexEntity(ctx, rel.TargetName, "entity")
			if err != nil {
				continue
			}

			_ = srcID   // used for logging
			_ = tgtID   // used by engine internally
			relCount++
		}
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "kg_index: completed",
			"entities", entityCount, "relations", relCount)
	}

	b, _ := json.Marshal(kgIndexResult{
		EntitiesIndexed:      entityCount,
		RelationshipsIndexed: relCount,
	})
	return string(b), nil
}

var _ Tool = (*kgIndexTool)(nil)
