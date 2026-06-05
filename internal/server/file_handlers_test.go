package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
)

func TestHTTP_FileHandlers_RootsAndToggle(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create SQLite DB
	dbPath := filepath.Join(tempDir, "entries.db")
	sdb, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite DB: %v", err)
	}
	defer sdb.Close()

	// 2. Initialize teamstore and project
	store := teamstore.NewStore(filepath.Join(tempDir, "groups"), filepath.Join(tempDir, "agents"), sdb)
	ctx := context.Background()
	projPath := filepath.Join(tempDir, "project-1")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	err = store.CreateProject(ctx, &teamstore.Project{
		ID:   "p1",
		Name: "Project One",
		Path: projPath,
	})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// 3. Create Mux
	mux := NewMux(tempDir, nil, WithTeamStore(store))
	defer mux.Close()

	// 4. Test GET /api/files/roots
	{
		req := httptest.NewRequest("GET", "/api/files/roots", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}

		var roots []FileRoot
		if err := json.Unmarshal(rec.Body.Bytes(), &roots); err != nil {
			t.Fatalf("failed to parse roots: %v", err)
		}

		// Should contain Global Plans and Project One
		hasGlobal := false
		hasProject := false
		for _, r := range roots {
			if r.Label == "Global Plans" {
				hasGlobal = true
			}
			if r.Label == "Project One" && r.Path == projPath && r.Group == "Projects" {
				hasProject = true
			}
		}

		if !hasGlobal {
			t.Error("missing Global Plans root")
		}
		if !hasProject {
			t.Error("missing Project One root")
		}
	}

	// 5. Test POST /api/files/toggle-checkbox
	{
		// Create a plan file under global plan directory
		planDir := filepath.Join(tempDir, "plan")
		if err := os.MkdirAll(planDir, 0755); err != nil {
			t.Fatalf("failed to create plan dir: %v", err)
		}
		planFile := filepath.Join(planDir, "test-plan.md")
		content := `# Test Plan

## Tasks
- [ ] Task 1
- [/] Task 2
- [x] Task 3
`
		if err := os.WriteFile(planFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write plan file: %v", err)
		}

		// Toggle Task 1 (index 0: from [ ] to [x])
		body := map[string]any{
			"path":  planFile,
			"index": 0,
		}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/files/toggle-checkbox", bytes.NewReader(data))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Read file back and check content
		updatedBytes, err := os.ReadFile(planFile)
		if err != nil {
			t.Fatalf("failed to read updated file: %v", err)
		}
		updatedContent := string(updatedBytes)
		if !strings.Contains(updatedContent, "- [x] Task 1") {
			t.Errorf("Task 1 was not checked: %s", updatedContent)
		}

		// Toggle Task 3 (index 2: from [x] to [ ])
		body = map[string]any{
			"path":  planFile,
			"index": 2,
		}
		data, _ = json.Marshal(body)
		req = httptest.NewRequest("POST", "/api/files/toggle-checkbox", bytes.NewReader(data))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		updatedBytes, err = os.ReadFile(planFile)
		if err != nil {
			t.Fatalf("failed to read updated file: %v", err)
		}
		updatedContent = string(updatedBytes)
		if !strings.Contains(updatedContent, "- [ ] Task 3") {
			t.Errorf("Task 3 was not unchecked: %s", updatedContent)
		}
	}
}
