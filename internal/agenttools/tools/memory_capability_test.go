package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
)

type stubMemoryAccess struct {
	searchResult    *engine.SearchResultSet
	searchQuery     engine.SearchQuery
	ingestCandidate engine.MemoryCandidate
	ingestErr       error
	ingestResult    engine.IngestResult
}

func (s *stubMemoryAccess) Ingest(_ context.Context, candidate engine.MemoryCandidate) (engine.IngestResult, error) {
	s.ingestCandidate = candidate
	if s.ingestErr != nil {
		return engine.IngestResult{}, s.ingestErr
	}
	if s.ingestResult.Action != "" {
		return s.ingestResult, nil
	}
	return engine.IngestResult{Action: "insert", ContentHash: "hash", IsNew: true}, nil
}

func (s *stubMemoryAccess) Search(_ context.Context, query engine.SearchQuery) (*engine.SearchResultSet, error) {
	s.searchQuery = query
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

func TestRecallMemoryForwardsHistoricalAsOf(t *testing.T) {
	access := &stubMemoryAccess{}
	tool := newRecallMemoryTool(Config{MemoryAccess: access})
	if _, err := tool.Execute(context.Background(), `{"query":"runtime version","as_of":"2026-08-10 09:30"}`); err != nil {
		t.Fatal(err)
	}
	if access.searchQuery.AsOf != "2026-08-10T09:30:00Z" {
		t.Fatalf("SearchQuery.AsOf = %q", access.searchQuery.AsOf)
	}
}

func TestRememberForwardsRevisionIdentity(t *testing.T) {
	access := &stubMemoryAccess{}
	tool := newRememberTool(Config{MemoryAccess: access})
	_, err := tool.Execute(context.Background(), `{
		"content":"SoloQueue uses Go 1.25.8.",
		"memory_type":"stable_fact",
		"subject_key":"project.runtime.go_version",
		"replaces_content_hash":"old-hash"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if access.ingestCandidate.SubjectKey != "project.runtime.go_version" ||
		access.ingestCandidate.ReplacesContentHash != "old-hash" {
		t.Fatalf("candidate = %+v", access.ingestCandidate)
	}
}

func TestRememberExposesSubjectConflict(t *testing.T) {
	tool := newRememberTool(Config{MemoryAccess: &stubMemoryAccess{ingestErr: engine.ErrMemorySubjectConflict}})
	_, err := tool.Execute(context.Background(), `{
		"content":"SoloQueue uses Go 1.25.8.",
		"memory_type":"stable_fact",
		"subject_key":"project.runtime.go_version"
	}`)
	if err == nil || err.Error() != "memory_subject_conflict" {
		t.Fatalf("Remember error = %v", err)
	}
}

func TestRememberReportsReplacementAsSaved(t *testing.T) {
	access := &stubMemoryAccess{ingestResult: engine.IngestResult{
		Action: "replace", ContentHash: "new-hash", IsNew: true,
	}}
	tool := newRememberTool(Config{MemoryAccess: access})
	result, err := tool.Execute(context.Background(), `{
		"content":"SoloQueue uses Go 1.25.8.",
		"memory_type":"stable_fact",
		"subject_key":"project.runtime.go_version",
		"replaces_content_hash":"old-hash"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"saved":true`) || !strings.Contains(result, `"action":"replace"`) {
		t.Fatalf("Remember result = %s", result)
	}
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
