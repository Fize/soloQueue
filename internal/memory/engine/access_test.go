package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestBoundAccessIsolatesL1AndL2Groups(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	groupAID := uuid.NewString()
	groupBID := uuid.NewString()
	groupA, err := e.BindL2Group(groupAID)
	if err != nil {
		t.Fatal(err)
	}
	groupASecondSession, err := e.BindL2Group(groupAID)
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := e.BindL2Group(groupBID)
	if err != nil {
		t.Fatal(err)
	}
	l1 := e.BindL1(ScopeGlobal, "", true)

	remember := func(t *testing.T, access Access, content string) string {
		t.Helper()
		result, err := access.Ingest(ctx, MemoryCandidate{
			Content: content, MemoryType: MemoryTypeStableFact,
			SourceType: SourceExplicit, ExplicitUserRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsNew {
			t.Fatalf("memory %q was not inserted: %+v", content, result)
		}
		return result.ContentHash
	}

	const identicalContent = "shared keyword identical content"
	l1Hash := remember(t, l1, identicalContent)
	groupAHash := remember(t, groupA, identicalContent)
	groupBHash := remember(t, groupB, identicalContent)
	if l1Hash == groupAHash || groupAHash == groupBHash || l1Hash == groupBHash {
		t.Fatal("content hashes must be owner-specific")
	}

	assertOnly := func(t *testing.T, access Access, wantHash string) {
		t.Helper()
		result, err := access.Search(ctx, SearchQuery{Text: "shared keyword", Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Results) != 1 || result.Results[0].ContentHash != wantHash || result.Results[0].Content != identicalContent {
			t.Fatalf("got memories %+v, want only hash %q", result.Results, wantHash)
		}
	}
	assertOnly(t, l1, l1Hash)
	assertOnly(t, groupASecondSession, groupAHash)
	assertOnly(t, groupB, groupBHash)
	timeline, err := groupASecondSession.Timeline(ctx, "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 1 || timeline[0].ContentHash != groupAHash {
		t.Fatalf("group A timeline leaked or missed memory: %+v", timeline)
	}
}

func TestBoundAccessOverridesUntrustedOwnerAndScope(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	access, err := e.BindL2Group(ownerID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := access.Ingest(ctx, MemoryCandidate{
		Content: "scope override proof", MemoryType: MemoryTypeDecision,
		SourceType: SourceExplicit, ExplicitUserRequest: true,
		OwnerType: OwnerL1, ScopeType: ScopeGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}

	var ownerType, storedOwnerID, scopeType, scopeID string
	if err := e.db.QueryRowContext(ctx, `
		SELECT owner_type, owner_id, scope_type, scope_id
		FROM mem_entries WHERE content_hash = ?`, result.ContentHash,
	).Scan(&ownerType, &storedOwnerID, &scopeType, &scopeID); err != nil {
		t.Fatal(err)
	}
	if ownerType != OwnerL2Group || storedOwnerID != ownerID || scopeType != ScopeTeam || scopeID != ownerID {
		t.Fatalf("unexpected bound ownership: %q %q %q %q", ownerType, storedOwnerID, scopeType, scopeID)
	}
}

func TestBoundAccessKnowledgeGraphIsOwnerScoped(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	groupA, _ := e.BindL2Group(uuid.NewString())
	groupB, _ := e.BindL2Group(uuid.NewString())

	for _, tc := range []struct {
		access  Access
		content string
		target  string
	}{
		{groupA, "alpha graph memory", "AlphaTarget"},
		{groupB, "beta graph memory", "BetaTarget"},
	} {
		_, err := tc.access.Ingest(ctx, MemoryCandidate{
			Content: tc.content, MemoryType: MemoryTypeStableFact,
			SourceType: SourceExplicit, ExplicitUserRequest: true,
			Entities: []EntityExtraction{{
				Name:      "SharedEntity",
				Relations: []RelationExtraction{{TargetName: tc.target, RelType: "uses"}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	result, err := groupA.Search(ctx, SearchQuery{Text: "SharedEntity", Entities: []string{"SharedEntity"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].Content != "alpha graph memory" {
		t.Fatalf("group A graph search leaked or missed memory: %+v", result.Results)
	}
}

func TestBindL2GroupRejectsInvalidOwner(t *testing.T) {
	if _, err := newTestEngine(t).BindL2Group("not-a-uuid"); err == nil {
		t.Fatal("expected invalid owner error")
	}
}
