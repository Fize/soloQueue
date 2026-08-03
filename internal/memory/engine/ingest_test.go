package engine

import (
	"context"
	"testing"
)

func TestIngestSkipsRoutineTaskResult(t *testing.T) {
	engine := newTestEngine(t)
	result, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "Build completed successfully and tests passed.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "skip" || result.Reason != "routine task result" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestIngestExplicitRequestBypassesRoutineFilter(t *testing.T) {
	engine := newTestEngine(t)
	result, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "Remember that release builds are completed on the signed runner.",
		MemoryType:          MemoryTypeDecision,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceExplicit,
		ExplicitUserRequest: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "insert" || !result.IsNew {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestIngestDeduplicatesNormalizedContentWithinScope(t *testing.T) {
	engine := newTestEngine(t)
	base := MemoryCandidate{
		Content:    "Project uses SQLite for durable storage.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
	}
	first, err := engine.Ingest(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	base.Content = "  PROJECT   uses sqlite for durable storage. "
	second, err := engine.Ingest(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if first.Action != "insert" || second.Action != "skip" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if first.ContentHash != second.ContentHash {
		t.Fatalf("duplicate should return canonical target hash")
	}
}

func TestSearchFiltersScopeAndArchivedMemories(t *testing.T) {
	engine := newTestEngine(t)
	for _, scopeID := range []string{"/work/a", "/work/b"} {
		_, err := engine.Ingest(context.Background(), MemoryCandidate{
			Content:    "Project database uses SQLite.",
			MemoryType: MemoryTypeStableFact,
			ScopeType:  ScopeProject,
			ScopeID:    scopeID,
			SourceType: SourceAgent,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	results, err := engine.Search(context.Background(), SearchQuery{
		Text:      "database SQLite",
		Limit:     10,
		ScopeType: ScopeProject,
		ScopeID:   "/work/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Results) != 1 || results.Results[0].ScopeID != "/work/a" {
		t.Fatalf("unexpected scoped results: %+v", results.Results)
	}
}

func TestIngestNormalizesUnknownEntityTypes(t *testing.T) {
	engine := newTestEngine(t)
	_, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue uses SQLite.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/soloQueue",
		SourceType: SourceAgent,
		Entities: []EntityExtraction{
			{Name: "SoloQueue", Type: "invented_project_kind"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := engine.Graph().GetNode(context.Background(), "SoloQueue")
	if err != nil {
		t.Fatal(err)
	}
	if node.Type != "entity" {
		t.Fatalf("unknown entity type should normalize to entity, got %q", node.Type)
	}
}
