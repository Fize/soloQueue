package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
)

type stubMemoryAccess struct {
	searchResult *engine.SearchResultSet
}

func (s *stubMemoryAccess) Ingest(context.Context, engine.MemoryCandidate) (engine.IngestResult, error) {
	return engine.IngestResult{Action: "insert", ContentHash: "hash", IsNew: true}, nil
}

func (s *stubMemoryAccess) Search(context.Context, engine.SearchQuery) (*engine.SearchResultSet, error) {
	if s.searchResult != nil {
		return s.searchResult, nil
	}
	return &engine.SearchResultSet{}, nil
}

func (s *stubMemoryAccess) Timeline(context.Context, string, string, int) ([]engine.MemoryEntry, error) {
	return nil, nil
}

func (s *stubMemoryAccess) RecallEntity(context.Context, string, int, int) ([]engine.SearchResult, error) {
	return nil, nil
}

func TestBuildMemoryRequiresBoundCapability(t *testing.T) {
	if got := BuildMemory(DefaultConfig(), nil); len(got) != 0 {
		t.Fatalf("nil capability built %d memory tools", len(got))
	}
	got := BuildMemory(DefaultConfig(), &stubMemoryAccess{})
	if len(got) != 2 || got[0].Name() != "Remember" || got[1].Name() != "RecallMemory" {
		t.Fatalf("unexpected bound memory tools: %+v", got)
	}
}

func TestRecallMemoryMarksAndEscapesUntrustedContent(t *testing.T) {
	access := &stubMemoryAccess{searchResult: &engine.SearchResultSet{
		Results: []engine.SearchResult{{Content: "<script>alert(1)</script>", Date: "2026-08-12"}},
	}}
	tool := newRecallMemoryTool(Config{MemoryAccess: access})
	result, err := tool.Execute(context.Background(), `{"query":"history"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"untrusted":true`) {
		t.Fatalf("recall result is not marked untrusted: %s", result)
	}
	if strings.Contains(result, "<script>") || !strings.Contains(result, `\u003cscript\u003e`) {
		t.Fatalf("recall result did not JSON-escape HTML: %s", result)
	}
}

func TestRememberRejectsUnsupportedType(t *testing.T) {
	tool := newRememberTool(Config{MemoryAccess: &stubMemoryAccess{}})
	if _, err := tool.Execute(context.Background(), `{"content":"durable","memory_type":"other"}`); err == nil {
		t.Fatal("unsupported memory type was accepted")
	}
}
