package engine

import "time"

// MemoryEntry is a stored text memory with metadata.
type MemoryEntry struct {
	ID             string  `json:"id"`
	Content        string  `json:"content"`
	ContentHash    string  `json:"content_hash"`
	Date           string  `json:"date"`
	Tags           string  `json:"tags"`
	EventTime      string  `json:"event_time"`
	CreatedAt      string  `json:"created_at"`
	Salience       float64 `json:"salience"`
	LastRecalledAt string  `json:"last_recalled_at,omitempty"`
	MemoryType     string  `json:"memory_type"`
	ScopeType      string  `json:"scope_type"`
	ScopeID        string  `json:"scope_id"`
	SourceType     string  `json:"source_type"`
	SourceID       string  `json:"source_id"`
	Status         string  `json:"status"`
	Confidence     float64 `json:"confidence"`
	ExpiresAt      string  `json:"expires_at,omitempty"`
	SupersedesHash string  `json:"supersedes_hash,omitempty"`
	CanonicalHash  string  `json:"canonical_hash,omitempty"`
	LastUsedAt     string  `json:"last_used_at,omitempty"`
	OwnerType      string  `json:"-"`
	OwnerID        string  `json:"-"`
	SubjectKey     string  `json:"subject_key,omitempty"`
	ValidFrom      string  `json:"valid_from,omitempty"`
	ValidUntil     string  `json:"valid_until,omitempty"`
}

// GraphNode is an entity in the knowledge graph.
type GraphNode struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	MentionCount int     `json:"mention_count"`
	FirstSeen    string  `json:"first_seen"`
	LastSeen     string  `json:"last_seen"`
	Confidence   float64 `json:"confidence"`
}

// GraphEdge is a typed relation between two graph nodes.
type GraphEdge struct {
	ID         int64   `json:"id"`
	Source     int64   `json:"source"`
	Target     int64   `json:"target"`
	SourceName string  `json:"source_name"`
	TargetName string  `json:"target_name"`
	RelType    string  `json:"rel_type"`
	Weight     float64 `json:"weight"`
	Evidence   string  `json:"evidence"`
	SourceHash string  `json:"source_hash"`
	EventTime  string  `json:"event_time"`
	ValidFrom  string  `json:"valid_from,omitempty"`
	ValidUntil string  `json:"valid_until,omitempty"`
}

// GraphAlias maps an alias to a canonical entity name.
type GraphAlias struct {
	ID        int64  `json:"id"`
	Alias     string `json:"alias"`
	Canonical string `json:"canonical"`
}

// SearchResult is one ranked hit from a hybrid search.
type SearchResult struct {
	ContentHash    string  `json:"content_hash"`
	Content        string  `json:"content"`
	Score          float64 `json:"score"`
	Source         string  `json:"source"` // "bm25", "kg", or "vector"
	Date           string  `json:"date"`
	Tags           string  `json:"tags"`
	EventTime      string  `json:"event_time"`
	MemoryType     string  `json:"memory_type"`
	ScopeType      string  `json:"scope_type"`
	ScopeID        string  `json:"scope_id"`
	Status         string  `json:"status"`
	ExpiresAt      string  `json:"expires_at,omitempty"`
	SubjectKey     string  `json:"subject_key,omitempty"`
	ValidFrom      string  `json:"valid_from,omitempty"`
	ValidUntil     string  `json:"valid_until,omitempty"`
	SupersedesHash string  `json:"supersedes_hash,omitempty"`
	OwnerType      string  `json:"-"`
	OwnerID        string  `json:"-"`
}

// SearchQuery parameterizes a hybrid search.
type SearchQuery struct {
	Text                string   `json:"text"`
	Entities            []string `json:"entities,omitempty"`
	DateFrom            string   `json:"date_from,omitempty"`
	DateTo              string   `json:"date_to,omitempty"`
	AsOf                string   `json:"as_of,omitempty"`
	Limit               int      `json:"limit"`
	IncludeGraphContext bool     `json:"include_graph_context"`
	ScopeType           string   `json:"scope_type,omitempty"`
	ScopeID             string   `json:"scope_id,omitempty"`
	IncludeGlobal       bool     `json:"include_global,omitempty"`
	OwnerType           string   `json:"-"`
	OwnerID             string   `json:"-"`
}

const (
	MemoryTypePreference       = "preference"
	MemoryTypeDecision         = "decision"
	MemoryTypeStableFact       = "stable_fact"
	MemoryTypeReusableSolution = "reusable_solution"
	MemoryTypeLegacy           = "legacy"

	ScopeGlobal     = "global"
	ScopeProject    = "project"
	ScopeTeam       = "team"
	ScopeSimulation = "simulation"

	SourceExplicit   = "explicit"
	SourceAgent      = "agent"
	SourceCompaction = "compaction"
	SourceMigration  = "migration"
	SourceSimulation = "simulation"

	StatusActive      = "active"
	StatusArchived    = "archived"
	StatusQuarantined = "quarantined"
	StatusSuperseded  = "superseded"

	OwnerL1      = "l1"
	OwnerL2Group = "l2_group"
)

// MemoryCandidate is the only supported input for new long-term memories.
type MemoryCandidate struct {
	Content             string
	SubjectKey          string
	ReplacesContentHash string
	MemoryType          string
	ScopeType           string
	ScopeID             string
	SourceType          string
	SourceID            string
	Date                string
	EventTime           string
	Confidence          float64
	ExpiresAt           string
	ExplicitUserRequest bool
	Entities            []EntityExtraction
	OwnerType           string
	OwnerID             string
}

// IngestResult explains whether a candidate was stored or rejected.
type IngestResult struct {
	Action      string `json:"action"`
	ContentHash string `json:"content_hash,omitempty"`
	IsNew       bool   `json:"is_new"`
	Reason      string `json:"reason,omitempty"`
}

// SearchResultSet wraps hybrid search output.
type SearchResultSet struct {
	Results      []SearchResult `json:"results"`
	BM25Count    int            `json:"bm25_count"`
	KGCount      int            `json:"kg_count"`
	VectorCount  int            `json:"vector_count"`
	GraphEdges   []GraphEdge    `json:"graph_edges,omitempty"`
	QueryLatency time.Duration  `json:"query_latency"`
}

// ConsolidationReport summarizes maintenance operations.
type ConsolidationReport struct {
	EdgesDecayed         int               `json:"edges_decayed"`
	EntitiesMerged       int               `json:"entities_merged"`
	StaleMemoriesRemoved int               `json:"stale_memories_removed"`
	DedupSuggestions     []DedupSuggestion `json:"dedup_suggestions,omitempty"`
	Communities          [][]string        `json:"communities,omitempty"`
	CommunitiesFound     int               `json:"communities_found"`
}

// DedupSuggestion is a pair of potentially duplicate content hashes.
type DedupSuggestion struct {
	Hash1      string  `json:"hash1"`
	Hash2      string  `json:"hash2"`
	Similarity float64 `json:"similarity"`
}

// EntityExtraction is input from the agent for KG indexing.
type EntityExtraction struct {
	Name       string               `json:"name"`
	Type       string               `json:"type,omitempty"`
	Confidence float64              `json:"confidence,omitempty"`
	Relations  []RelationExtraction `json:"relations,omitempty"`
}

// RelationExtraction is a relationship between two entities.
type RelationExtraction struct {
	TargetName string  `json:"target_name"`
	RelType    string  `json:"rel_type"`
	Weight     float64 `json:"weight,omitempty"`
}
