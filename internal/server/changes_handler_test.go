package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func TestHTTP_GetSessionChanges_TildeExpansion(t *testing.T) {
	// Setup custom HOME directory for tilde expansion
	tempHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", origHome)

	// Create a real project directory inside tempHome
	projectPath := filepath.Join(tempHome, "my-project")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatalf("Failed to create project path: %v", err)
	}

	// Initialize git repo in projectPath
	cmd := exec.Command("git", "init")
	cmd.Dir = projectPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Set git config for testing
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = projectPath
	_ = cmd.Run()
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = projectPath
	_ = cmd.Run()

	// Create and commit a base file
	filePath := filepath.Join(projectPath, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Write file failed: %v", err)
	}

	cmd = exec.Command("git", "add", "main.go")
	cmd.Dir = projectPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = projectPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Capture the git HEAD ref
	refCmd := exec.Command("git", "rev-parse", "HEAD")
	refCmd.Dir = projectPath
	refOutput, err := refCmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	baseRef := string(refOutput)

	// Now modify the file to introduce a diff
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0644); err != nil {
		t.Fatalf("Write modified file failed: %v", err)
	}

	// Setup Server and L2 store
	logDir := t.TempDir()
	log, _ := logger.System(logDir, logger.WithConsole(false), logger.WithFile(false))

	l2Store := session.NewL2SessionStore(nil, tempHome, log)
	
	// Create an L2 session with a tilde-prefixed WorkDir: "~/my-project"
	sessionID := "test-session-uuid"
	info, err := l2Store.Create(context.Background(), sessionID, "dev", "test-proj", "~/my-project")
	if err != nil {
		t.Fatalf("Failed to create L2 session: %v", err)
	}

	entry := l2Store.GetEntry(info.ID)
	if entry == nil {
		t.Fatalf("Failed to get session entry")
	}
	entry.GitBaseRef = baseRef

	mux := NewMux(tempHome, log, WithL2SessionStore(l2Store))
	defer mux.Close()

	// Make request to the changes endpoint
	req := httptest.NewRequest("GET", "/api/session/l2/"+sessionID+"/changes", nil)
	// Setup chi route context so chi.URLParam works
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", sessionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp ChangesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if !resp.IsGitRepo {
		t.Errorf("Expected IsGitRepo to be true, got false")
	}

	if len(resp.Changes) == 0 {
		t.Fatalf("Expected changes, got none")
	}

	foundMain := false
	for _, change := range resp.Changes {
		if change.Path == "main.go" {
			foundMain = true
			if change.Status != "modified" {
				t.Errorf("Expected status modified, got %s", change.Status)
			}
			if change.Additions == 0 {
				t.Errorf("Expected additions, got 0")
			}
		}
	}

	if !foundMain {
		t.Errorf("Expected main.go to be in changed files list")
	}
}
