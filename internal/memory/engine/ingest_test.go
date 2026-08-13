package engine

import (
	"context"
	"errors"
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

func TestIngestStoresNormalizedSubjectAndValidityStart(t *testing.T) {
	engine := newTestEngine(t)
	result, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue uses Go 1.25.8.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-13T09:30:00Z",
		SubjectKey: "  Project.Runtime.Go_Version  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	var subjectKey, validFrom string
	if err := engine.store.db.QueryRow(`
		SELECT subject_key, valid_from FROM mem_entries WHERE content_hash = ?
	`, result.ContentHash).Scan(&subjectKey, &validFrom); err != nil {
		t.Fatal(err)
	}
	if subjectKey != "project.runtime.go_version" || validFrom != "2026-08-13T09:30:00Z" {
		t.Fatalf("subject_key=%q valid_from=%q", subjectKey, validFrom)
	}
}

func TestIngestRejectsInvalidSubjectKey(t *testing.T) {
	engine := newTestEngine(t)
	result, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue uses Go 1.25.8.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		SubjectKey: "project runtime/go version",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "skip" || result.Reason != "invalid subject key" {
		t.Fatalf("result = %+v", result)
	}
}

func TestIngestRejectsSecondActiveMutableMemoryForSubject(t *testing.T) {
	engine := newTestEngine(t)
	base := MemoryCandidate{
		Content:    "SoloQueue uses Go 1.24.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T09:00:00Z",
		SubjectKey: "project.runtime.go_version",
	}
	if _, err := engine.Ingest(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	base.Content = "SoloQueue uses Go 1.25.8."
	base.EventTime = "2026-08-13T09:00:00Z"
	if _, err := engine.Ingest(context.Background(), base); !errors.Is(err, ErrMemorySubjectConflict) {
		t.Fatalf("Ingest error = %v, want ErrMemorySubjectConflict", err)
	}
	var count int
	if err := engine.store.db.QueryRow(`
		SELECT COUNT(*) FROM mem_entries
		WHERE status = 'active' AND subject_key = 'project.runtime.go_version'
	`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active subject count = %d, want 1", count)
	}
}

func TestIngestAtomicallyReplacesActiveMemory(t *testing.T) {
	engine := newTestEngine(t)
	old, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue uses Go 1.24.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T09:00:00Z",
		SubjectKey: "project.runtime.go_version",
	})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "SoloQueue uses Go 1.25.8.",
		MemoryType:          MemoryTypeStableFact,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceAgent,
		EventTime:           "2026-08-13T09:00:00Z",
		SubjectKey:          "project.runtime.go_version",
		ReplacesContentHash: old.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if newer.Action != "replace" || !newer.IsNew {
		t.Fatalf("replacement result = %+v", newer)
	}

	var oldStatus, oldReplacement, oldValidUntil string
	if err := engine.store.db.QueryRow(`
		SELECT status, supersedes_hash, valid_until
		FROM mem_entries WHERE content_hash = ?
	`, old.ContentHash).Scan(&oldStatus, &oldReplacement, &oldValidUntil); err != nil {
		t.Fatal(err)
	}
	if oldStatus != StatusSuperseded || oldReplacement != newer.ContentHash || oldValidUntil != "2026-08-13T09:00:00Z" {
		t.Fatalf("old status=%q replacement=%q valid_until=%q", oldStatus, oldReplacement, oldValidUntil)
	}
	var newStatus string
	if err := engine.store.db.QueryRow(`
		SELECT status FROM mem_entries WHERE content_hash = ?
	`, newer.ContentHash).Scan(&newStatus); err != nil {
		t.Fatal(err)
	}
	if newStatus != StatusActive {
		t.Fatalf("new status = %q, want %q", newStatus, StatusActive)
	}
}

func TestIngestRejectsReplacementWithoutSubject(t *testing.T) {
	engine := newTestEngine(t)
	old, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue uses Go 1.24.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "SoloQueue uses Go 1.25.8.",
		MemoryType:          MemoryTypeStableFact,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceAgent,
		EventTime:           "2026-08-13T09:00:00Z",
		ReplacesContentHash: old.ContentHash,
	})
	if !errors.Is(err, ErrMemoryReplacementInvalid) {
		t.Fatalf("Ingest error = %v, want ErrMemoryReplacementInvalid", err)
	}
	var status string
	if err := engine.store.db.QueryRow(`
		SELECT status FROM mem_entries WHERE content_hash = ?
	`, old.ContentHash).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusActive {
		t.Fatalf("target status = %q, want %q", status, StatusActive)
	}
}

func TestIngestRejectsReplacementBeforeTargetValidity(t *testing.T) {
	engine := newTestEngine(t)
	old, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue uses Go 1.25.8.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-13T09:00:00Z",
		SubjectKey: "project.runtime.go_version",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "SoloQueue uses Go 1.24.",
		MemoryType:          MemoryTypeStableFact,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceAgent,
		EventTime:           "2026-08-01T09:00:00Z",
		SubjectKey:          "project.runtime.go_version",
		ReplacesContentHash: old.ContentHash,
	})
	if !errors.Is(err, ErrMemoryReplacementInvalid) {
		t.Fatalf("Ingest error = %v, want ErrMemoryReplacementInvalid", err)
	}
	var status string
	if err := engine.store.db.QueryRow(`SELECT status FROM mem_entries WHERE content_hash = ?`, old.ContentHash).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusActive {
		t.Fatalf("target status = %q, want %q", status, StatusActive)
	}
}

func TestIngestReplacementOfCanonicalDuplicateSkips(t *testing.T) {
	engine := newTestEngine(t)
	old, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue uses Go 1.25.8.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T09:00:00Z",
		SubjectKey: "project.runtime.go_version",
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "  SOLOQUEUE   uses go 1.25.8. ",
		MemoryType:          MemoryTypeStableFact,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceAgent,
		EventTime:           "2026-08-13T09:00:00Z",
		SubjectKey:          "project.runtime.go_version",
		ReplacesContentHash: old.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Action != "skip" || duplicate.ContentHash != old.ContentHash {
		t.Fatalf("duplicate replacement result = %+v", duplicate)
	}
	var status string
	if err := engine.store.db.QueryRow(`SELECT status FROM mem_entries WHERE content_hash = ?`, old.ContentHash).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusActive {
		t.Fatalf("target status = %q, want %q", status, StatusActive)
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

func TestSearchAsOfReturnsVersionValidAtRequestedTime(t *testing.T) {
	engine := newTestEngine(t)
	old, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue runtime uses Go version 1.24.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T09:00:00Z",
		SubjectKey: "project.runtime.go_version",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "SoloQueue runtime uses Go version 1.25.8.",
		MemoryType:          MemoryTypeStableFact,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceAgent,
		EventTime:           "2026-08-13T09:00:00Z",
		SubjectKey:          "project.runtime.go_version",
		ReplacesContentHash: old.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := engine.Search(context.Background(), SearchQuery{
		Text:      "SoloQueue runtime Go version",
		Limit:     10,
		ScopeType: ScopeProject,
		ScopeID:   "/work/project",
		AsOf:      "2026-08-10T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].ContentHash != old.ContentHash {
		t.Fatalf("historical results = %+v, want old hash %q", result.Results, old.ContentHash)
	}
}

func TestSearchAsOfComparesTimestampOffsetsChronologically(t *testing.T) {
	engine := newTestEngine(t)
	stored, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue runtime uses Go version 1.24.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T18:00:00+08:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Search(context.Background(), SearchQuery{
		Text:      "SoloQueue runtime Go version",
		Limit:     10,
		ScopeType: ScopeProject,
		ScopeID:   "/work/project",
		AsOf:      "2026-08-01T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].ContentHash != stored.ContentHash {
		t.Fatalf("historical results = %+v", result.Results)
	}
}

func TestContentHashesVisibleAtOwnedIncludesHistoricalVersion(t *testing.T) {
	engine := newTestEngine(t)
	old, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue runtime uses Go version 1.24.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T09:00:00Z",
		SubjectKey: "project.runtime.go_version",
	})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "SoloQueue runtime uses Go version 1.25.8.",
		MemoryType:          MemoryTypeStableFact,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceAgent,
		EventTime:           "2026-08-13T09:00:00Z",
		SubjectKey:          "project.runtime.go_version",
		ReplacesContentHash: old.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}

	visible := engine.store.ContentHashesVisibleAtOwned(
		context.Background(), []string{old.ContentHash, newer.ContentHash},
		OwnerL1, "", ScopeProject, "/work/project", false, "2026-08-10T09:00:00Z",
	)
	if !visible[old.ContentHash] || visible[newer.ContentHash] {
		t.Fatalf("historical visibility = %+v", visible)
	}
}

func TestSearchAsOfKeepsHistoricalGraphContext(t *testing.T) {
	engine := newTestEngine(t)
	old, err := engine.Ingest(context.Background(), MemoryCandidate{
		Content:    "SoloQueue runtime uses Go version 1.24.",
		MemoryType: MemoryTypeStableFact,
		ScopeType:  ScopeProject,
		ScopeID:    "/work/project",
		SourceType: SourceAgent,
		EventTime:  "2026-08-01T09:00:00Z",
		SubjectKey: "project.runtime.go_version",
		Entities: []EntityExtraction{{
			Name: "SoloQueue", Type: "project",
			Relations: []RelationExtraction{{TargetName: "Go", RelType: "uses"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Ingest(context.Background(), MemoryCandidate{
		Content:             "SoloQueue runtime uses Go version 1.25.8.",
		MemoryType:          MemoryTypeStableFact,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		SourceType:          SourceAgent,
		EventTime:           "2026-08-13T09:00:00Z",
		SubjectKey:          "project.runtime.go_version",
		ReplacesContentHash: old.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := engine.Search(context.Background(), SearchQuery{
		Text:                "SoloQueue runtime Go version",
		Entities:            []string{"SoloQueue"},
		Limit:               10,
		IncludeGraphContext: true,
		ScopeType:           ScopeProject,
		ScopeID:             "/work/project",
		AsOf:                "2026-08-10T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.GraphEdges) != 1 || result.GraphEdges[0].SourceHash != old.ContentHash {
		t.Fatalf("historical graph context = %+v", result.GraphEdges)
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
