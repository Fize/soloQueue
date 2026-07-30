package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
)

func setupResolveProjectTool(t *testing.T) (*resolveProjectTool, func()) {
	t.Helper()

	db, err := sqlitedb.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) = %v", err)
	}

	groupsDir := t.TempDir()
	agentsDir := t.TempDir()
	store := teamstore.NewStore(groupsDir, agentsDir, db)
	ctx := context.Background()

	projects := []teamstore.Project{
		{ID: "soloqueue", Name: "soloQueue", Path: "/Users/xiaobaitu/github.com/soloQueue", Description: "main project"},
		{ID: "blog", Name: "My Blog", Path: "/Users/xiaobaitu/github.com/blog", Description: "blog"},
		{ID: "docs", Name: "Documentation", Path: "/Users/xiaobaitu/github.com/docs", Description: "docs project"},
	}
	for _, p := range projects {
		cp := p
		if err := store.CreateProject(ctx, &cp); err != nil {
			db.Close()
			t.Fatalf("CreateProject: %v", err)
		}
	}

	cfg := Config{
		TeamStore: store,
		Runtime:   NewHostRuntime(),
	}
	tool := newResolveProjectTool(cfg)

	cleanup := func() {
		db.Close()
	}
	return tool, cleanup
}

func TestResolveProjectTool_Metadata(t *testing.T) {
	tool, cleanup := setupResolveProjectTool(t)
	defer cleanup()

	if tool.Name() != "resolve_project" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "resolve_project")
	}
	if tool.Description() == "" {
		t.Error("Description() is empty")
	}
	if tool.Parameters() == nil {
		t.Error("Parameters() is nil")
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
		t.Fatalf("Parameters() invalid JSON: %v", err)
	}
}

func TestResolveProjectTool_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("exact id match", func(t *testing.T) {
		tool, cleanup := setupResolveProjectTool(t)
		defer cleanup()

		result, err := tool.Execute(ctx, `{"query":"soloqueue"}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}

		var res resolveProjectResult
		if err := json.Unmarshal([]byte(result), &res); err != nil {
			t.Fatalf("invalid result JSON: %v", err)
		}
		if res.Ambiguous {
			t.Error("expected unambiguous result")
		}
		if res.Project == nil {
			t.Fatal("expected project")
		}
		if res.Project.ID != "soloqueue" {
			t.Errorf("ID = %q, want soloqueue", res.Project.ID)
		}
		if res.ResolvedPath == "" {
			t.Error("resolved_path is empty")
		}
	})

	t.Run("case-insensitive name match", func(t *testing.T) {
		tool, cleanup := setupResolveProjectTool(t)
		defer cleanup()

		result, err := tool.Execute(ctx, `{"query":"SOLOQUEUE"}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		var res resolveProjectResult
		json.Unmarshal([]byte(result), &res)
		if res.Project == nil || res.Project.ID != "soloqueue" {
			t.Errorf("expected soloqueue, got %+v", res.Project)
		}
	})

	t.Run("nonexistent returns error", func(t *testing.T) {
		tool, cleanup := setupResolveProjectTool(t)
		defer cleanup()

		_, err := tool.Execute(ctx, `{"query":"nonexistent_xyz"}`)
		if err == nil {
			t.Fatal("expected error for nonexistent project")
		}
	})

	t.Run("empty query returns invalid args", func(t *testing.T) {
		tool, cleanup := setupResolveProjectTool(t)
		defer cleanup()

		_, err := tool.Execute(ctx, `{"query":""}`)
		if err == nil {
			t.Fatal("expected error for empty query")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected 'empty' in error, got: %v", err)
		}
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		tool, cleanup := setupResolveProjectTool(t)
		defer cleanup()

		_, err := tool.Execute(ctx, `bad json`)
		if err == nil {
			t.Fatal("expected error for bad JSON")
		}
	})

	t.Run("ambiguous when multiple matches", func(t *testing.T) {
		_, cleanup := setupResolveProjectTool(t)
		defer cleanup()

		// Create a separate store with duplicate-name projects
		db, err := sqlitedb.Open(":memory:")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer db.Close()
		store := teamstore.NewStore(t.TempDir(), t.TempDir(), db)

		extra := []teamstore.Project{
			{ID: "test1", Name: "Test One", Path: "/tmp/test1", Description: "t1"},
			{ID: "test2", Name: "Test Two", Path: "/tmp/test2", Description: "t2"},
		}
		for _, p := range extra {
			cp := p
			if err := store.CreateProject(ctx, &cp); err != nil {
				t.Fatalf("CreateProject: %v", err)
			}
		}

		cfg := Config{
			TeamStore: store,
			Runtime:   NewHostRuntime(),
		}
		t2 := newResolveProjectTool(cfg)

		result, err := t2.Execute(ctx, `{"query":"test"}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		var res resolveProjectResult
		if err := json.Unmarshal([]byte(result), &res); err != nil {
			t.Fatalf("invalid result: %v", err)
		}
		if !res.Ambiguous {
			t.Error("expected ambiguous=true")
		}
		if len(res.Candidates) < 2 {
			t.Errorf("expected at least 2 candidates, got %d", len(res.Candidates))
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		tool, cleanup := setupResolveProjectTool(t)
		defer cleanup()

		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := tool.Execute(cancelCtx, `{"query":"blog"}`)
		if err == nil {
			t.Fatal("expected error for canceled context")
		}
	})
}
