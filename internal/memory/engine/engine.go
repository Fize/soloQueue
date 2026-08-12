package engine

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/vectorstore"
)

// Engine is the top-level memory engine combining BM25 + KG [+ optional Vector] search.
type Engine struct {
	store    *MemoryStore
	graph    *GraphStore
	searcher *HybridSearcher
	vector   *VectorSearcher

	db  *sql.DB
	mu  *sync.Mutex
	log *logger.Logger
}

// New creates a new Engine backed by the shared database.
// embedder and vecStore may be nil for no-embedding mode.
func New(db *sql.DB, mu *sync.Mutex, embedder embedding.Embedder, vecStore vectorstore.VectorStore, log *logger.Logger) *Engine {
	store := NewMemoryStore(db, mu, embedder, vecStore, log)
	graph := NewGraphStore(db, mu, log)
	bm25 := NewBM25Searcher(store)
	kg := NewGraphSearcher(graph)
	vector := NewVectorSearcher(embedder, vecStore)
	searcher := NewHybridSearcher(bm25, kg, vector, store)

	return &Engine{
		store:    store,
		graph:    graph,
		searcher: searcher,
		vector:   vector,
		db:       db,
		mu:       mu,
		log:      log,
	}
}

// Save stores a memory and returns its content hash.
func (e *Engine) Save(ctx context.Context, content, date, tags, eventTime string) (string, bool, error) {
	return e.store.Save(ctx, content, date, tags, eventTime)
}

// SaveWithEntities saves a memory and indexes entities/relations in the KG.
func (e *Engine) SaveWithEntities(ctx context.Context, content, date, tags, eventTime string, entities []EntityExtraction) (string, bool, error) {
	hash, isNew, err := e.store.Save(ctx, content, date, tags, eventTime)
	if err != nil {
		return hash, isNew, err
	}
	e.indexEntities(ctx, OwnerL1, "", content, hash, eventTime, entities)
	return hash, isNew, nil
}

func (e *Engine) indexEntities(ctx context.Context, ownerType, ownerID, content, hash, eventTime string, entities []EntityExtraction) {
	for _, entity := range entities {
		if entity.Confidence <= 0 {
			entity.Confidence = 1.0
		}
		nodeType := normalizeEntityType(entity.Type)

		srcID, _, err := e.graph.UpsertNodeOwned(ctx, ownerType, ownerID, entity.Name, nodeType, entity.Confidence)
		if err != nil {
			e.logWarn("kg index: upsert node", err)
			continue
		}

		for _, rel := range entity.Relations {
			weight := rel.Weight
			if weight <= 0 {
				weight = 1.0
			}
			tgtID, _, err := e.graph.UpsertNodeOwned(ctx, ownerType, ownerID, rel.TargetName, "entity", 0.5)
			if err != nil {
				e.logWarn("kg index: upsert target node", err)
				continue
			}
			if err := e.graph.UpsertEdgeOwned(ctx, ownerType, ownerID, srcID, tgtID, rel.RelType, weight, content, hash, eventTime, "", ""); err != nil {
				e.logWarn("kg index: upsert edge", err)
			}
		}
	}
}

// Search performs a hybrid search across all configured pipelines.
func (e *Engine) Search(ctx context.Context, query SearchQuery) (*SearchResultSet, error) {
	query.OwnerType, query.OwnerID = normalizeOwner(query.OwnerType, query.OwnerID)
	if !validOwner(query.OwnerType, query.OwnerID) {
		return nil, ErrMemoryOwnerInvalid
	}
	return e.searcher.Search(ctx, query)
}

// IndexEntity indexes an entity in the knowledge graph.
func (e *Engine) IndexEntity(ctx context.Context, name, nodeType string) (int64, error) {
	if nodeType == "" {
		nodeType = "entity"
	}
	id, _, err := e.graph.UpsertNode(ctx, name, nodeType, 1.0)
	return id, err
}

// ConnectEntities creates a relationship between two entities.
func (e *Engine) ConnectEntities(ctx context.Context, sourceName, targetName, relType string, weight float64, evidence, sourceHash string, eventTime time.Time, validFrom, validUntil *time.Time) error {
	srcID, _, err := e.graph.UpsertNode(ctx, sourceName, "entity", 0.5)
	if err != nil {
		return fmt.Errorf("connect: source: %w", err)
	}
	tgtID, _, err := e.graph.UpsertNode(ctx, targetName, "entity", 0.5)
	if err != nil {
		return fmt.Errorf("connect: target: %w", err)
	}

	vf, vu := "", ""
	if validFrom != nil {
		vf = validFrom.Format(time.RFC3339)
	}
	if validUntil != nil {
		vu = validUntil.Format(time.RFC3339)
	}

	et := eventTime.Format(time.RFC3339)
	return e.graph.UpsertEdge(ctx, srcID, tgtID, relType, weight, evidence, sourceHash, et, vf, vu)
}

// AddAlias registers an alias for an entity.
func (e *Engine) AddAlias(ctx context.Context, alias, canonical string) error {
	return e.graph.AddAlias(ctx, alias, canonical)
}

// RecallEntity traverses the KG from an entity and returns related memories.
func (e *Engine) RecallEntity(ctx context.Context, entityName string, maxHops int, limit int) ([]SearchResult, error) {
	return e.RecallEntityScoped(ctx, entityName, maxHops, limit, "", "", false)
}

func (e *Engine) RecallEntityScoped(ctx context.Context, entityName string, maxHops int, limit int, scopeType, scopeID string, includeGlobal bool) ([]SearchResult, error) {
	query := SearchQuery{
		Entities:      []string{entityName},
		Limit:         limit,
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		IncludeGlobal: includeGlobal,
	}
	result, err := e.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

// Graph returns the underlying graph store for direct traversal.
func (e *Engine) Graph() *GraphStore {
	return e.graph
}

// ShortestPath finds the shortest path between two entities in the KG.
func (e *Engine) ShortestPath(ctx context.Context, source, target string, maxDepth int) ([]GraphNode, []GraphEdge, error) {
	return e.graph.ShortestPath(ctx, source, target, maxDepth)
}

// Timeline returns memories chronologically within a date range.
func (e *Engine) Timeline(ctx context.Context, from, to string, limit int) ([]MemoryEntry, error) {
	return e.store.Timeline(ctx, from, to, limit)
}

func (e *Engine) TimelineScoped(ctx context.Context, from, to string, limit int, scopeType, scopeID string, includeGlobal bool) ([]MemoryEntry, error) {
	return e.store.TimelineScoped(ctx, from, to, limit, scopeType, scopeID, includeGlobal)
}

// Consolidate runs maintenance operations.
func (e *Engine) Consolidate(ctx context.Context) (*ConsolidationReport, error) {
	c := NewConsolidator(e.store, e.graph, e.log)
	return c.Run(ctx, 30, 90)
}

// BoostSalience manually boosts a memory's salience.
func (e *Engine) BoostSalience(ctx context.Context, contentHash string) error {
	return e.store.BoostSalience(ctx, contentHash)
}

// HasVector returns true if vector search is enabled.
func (e *Engine) HasVector() bool {
	return e.vector != nil && e.vector.Enabled()
}

func (e *Engine) logWarn(msg string, err error) {
	if e.log != nil && err != nil {
		e.log.WarnContext(context.Background(), logger.CatApp, msg, "err", err.Error())
	}
}
