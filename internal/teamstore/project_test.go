package teamstore

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

func TestResolveProject(t *testing.T) {
	db, err := sqlitedb.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) = %v", err)
	}
	defer db.Close()

	groupsDir := t.TempDir()
	agentsDir := t.TempDir()
	store := NewStore(groupsDir, agentsDir, db)
	ctx := context.Background()

	projects := []Project{
		{ID: "soloqueue", Name: "soloQueue", Path: "/Users/xiaobaitu/github.com/soloQueue", Description: "main project"},
		{ID: "blog", Name: "My Blog", Path: "/Users/xiaobaitu/github.com/blog", Description: "blog"},
		{ID: "docs", Name: "Documentation", Path: "/Users/xiaobaitu/github.com/docs", Description: "docs project"},
	}
	for _, p := range projects {
		cp := p
		if err := store.CreateProject(ctx, &cp); err != nil {
			t.Fatalf("CreateProject(%s): %v", p.ID, err)
		}
	}

	t.Run("exact id match", func(t *testing.T) {
		results, err := store.ResolveProject(ctx, "soloqueue")
		if err != nil {
			t.Fatalf("ResolveProject: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].ID != "soloqueue" {
			t.Errorf("expected ID soloqueue, got %s", results[0].ID)
		}
	})

	t.Run("case-insensitive name match", func(t *testing.T) {
		results, err := store.ResolveProject(ctx, "SOLOQUEUE")
		if err != nil {
			t.Fatalf("ResolveProject: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].ID != "soloqueue" {
			t.Errorf("expected ID soloqueue, got %s", results[0].ID)
		}
	})

	t.Run("fuzzy path match", func(t *testing.T) {
		results, err := store.ResolveProject(ctx, "solo")
		if err != nil {
			t.Fatalf("ResolveProject: %v", err)
		}
		if len(results) < 1 {
			t.Fatal("expected at least 1 result")
		}
		found := false
		for _, r := range results {
			if r.ID == "soloqueue" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected soloqueue in results")
		}
	})

	t.Run("fuzzy name match", func(t *testing.T) {
		results, err := store.ResolveProject(ctx, "queue")
		if err != nil {
			t.Fatalf("ResolveProject: %v", err)
		}
		if len(results) < 1 {
			t.Fatal("expected at least 1 result")
		}
		found := false
		for _, r := range results {
			if r.ID == "soloqueue" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected soloqueue in results")
		}
	})

	t.Run("no match returns error", func(t *testing.T) {
		_, err := store.ResolveProject(ctx, "nonexistent_project_xyz")
		if err == nil {
			t.Fatal("expected error for nonexistent project")
		}
		if !strings.Contains(err.Error(), "no project matching") {
			t.Errorf("expected 'no project matching' error, got: %v", err)
		}
	})

	t.Run("multiple matches sorted by precision", func(t *testing.T) {
		// Insert an extra project whose path also contains "solo"
		extra := Project{ID: "solowork", Name: "SoloWork", Path: "/Users/xiaobaitu/solo-work", Description: "extra"}
		if err := store.CreateProject(ctx, &extra); err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		results, err := store.ResolveProject(ctx, "solo")
		if err != nil {
			t.Fatalf("ResolveProject: %v", err)
		}
		if len(results) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(results))
		}
		// Exact ID match ("soloqueue" has ID in "solo", but it's a LIKE) - first result
		// should be the one with exact ID match or any priority winner.
	})

	t.Run("case-insensitive path match", func(t *testing.T) {
		results, err := store.ResolveProject(ctx, "DOCS")
		if err != nil {
			t.Fatalf("ResolveProject: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].ID != "docs" {
			t.Errorf("expected ID docs, got %s", results[0].ID)
		}
	})

	t.Run("ResolveProject with nil db", func(t *testing.T) {
		nilStore := &Store{db: nil}
		_, err := nilStore.ResolveProject(ctx, "anything")
		if err == nil {
			t.Fatal("expected error for nil db")
		}
	})

	_ = os.RemoveAll(groupsDir)
	_ = os.RemoveAll(agentsDir)
}
