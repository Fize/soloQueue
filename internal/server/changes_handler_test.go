package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func TestHTTP_GetSessionChanges_TildeExpansion(t *testing.T) {
	// Setup custom HOME directory for tilde expansion
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

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
	req := newLocalhostRequest("GET", "/api/session/l2/"+sessionID+"/changes", nil)
	// Setup chi route context so chi.URLParam works
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", sessionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"hunks"`)) {
		t.Fatalf("summary response must omit hunks: %s", rec.Body.String())
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
			if change.Revision == "" {
				t.Error("Expected a stable per-file revision")
			}
		}
	}

	if !foundMain {
		t.Errorf("Expected main.go to be in changed files list")
	}

	diffReq := newLocalhostRequest("GET", "/api/session/l2/"+sessionID+"/changes/diff?path=main.go&expected_revision="+resp.Changes[0].Revision, nil)
	diffCtx := chi.NewRouteContext()
	diffCtx.URLParams.Add("id", sessionID)
	diffReq = diffReq.WithContext(context.WithValue(diffReq.Context(), chi.RouteCtxKey, diffCtx))
	diffRec := httptest.NewRecorder()
	mux.ServeHTTP(diffRec, diffReq)
	if diffRec.Code != http.StatusOK {
		t.Fatalf("Expected diff status 200, got %d, body: %s", diffRec.Code, diffRec.Body.String())
	}
	var diff FileChange
	if err := json.Unmarshal(diffRec.Body.Bytes(), &diff); err != nil {
		t.Fatalf("Failed to unmarshal diff response: %v", err)
	}
	if diff.Path != "main.go" || len(diff.Hunks) == 0 {
		t.Fatalf("Expected only main.go with hunks, got %#v", diff)
	}
	if diff.Revision != resp.Changes[0].Revision {
		t.Fatalf("Expected summary and diff revisions to match: %q != %q", diff.Revision, resp.Changes[0].Revision)
	}

	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() { println(\"newer\") }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	staleReq := newLocalhostRequest("GET", "/api/session/l2/"+sessionID+"/changes/diff?path=main.go&expected_revision="+resp.Changes[0].Revision, nil)
	staleCtx := chi.NewRouteContext()
	staleCtx.URLParams.Add("id", sessionID)
	staleReq = staleReq.WithContext(context.WithValue(staleReq.Context(), chi.RouteCtxKey, staleCtx))
	staleRec := httptest.NewRecorder()
	mux.ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("Expected stale revision status 409, got %d, body: %s", staleRec.Code, staleRec.Body.String())
	}

	invalidReq := newLocalhostRequest("GET", "/api/session/l2/"+sessionID+"/changes/diff?path=../secret", nil)
	invalidCtx := chi.NewRouteContext()
	invalidCtx.URLParams.Add("id", sessionID)
	invalidReq = invalidReq.WithContext(context.WithValue(invalidReq.Context(), chi.RouteCtxKey, invalidCtx))
	invalidRec := httptest.NewRecorder()
	mux.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("Expected invalid diff path to be rejected, got %d", invalidRec.Code)
	}
}

func TestComputeSnapshotSummaryOmitsDiffHunks(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "notes.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes := computeSnapshotSummary(workDir, map[string]string{"notes.txt": "old-hash"})
	if len(changes) != 1 {
		t.Fatalf("expected one change, got %#v", changes)
	}
	if changes[0].Revision == "" || changes[0].Additions != 3 {
		t.Fatalf("expected revision and summary counts, got %#v", changes[0])
	}
	if len(changes[0].Hunks) != 0 {
		t.Fatalf("summary must not build hunks: %#v", changes[0].Hunks)
	}
}

func TestComputeSnapshotDiffForPathBuildsOnlyRequestedFile(t *testing.T) {
	workDir := t.TempDir()
	for path, content := range map[string]string{
		"requested.txt": "requested content\n",
		"other.txt":     "other content must not be returned\n",
	} {
		if err := os.WriteFile(filepath.Join(workDir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := map[string]string{
		"requested.txt": "old-requested-hash",
		"other.txt":     "old-other-hash",
	}
	diff, ok := computeSnapshotDiffForPath(workDir, snapshot, "requested.txt")
	if !ok {
		t.Fatal("expected requested path to be found")
	}
	if diff.Path != "requested.txt" || len(diff.Hunks) != 1 {
		t.Fatalf("expected only requested diff, got %#v", diff)
	}
	for _, line := range diff.Hunks[0].Lines {
		if strings.Contains(line.Content, "other content") {
			t.Fatalf("unexpected content from non-requested file: %#v", diff.Hunks)
		}
	}
	if _, ok := computeSnapshotDiffForPath(workDir, snapshot, "missing.txt"); ok {
		t.Fatal("path outside the summary must be rejected")
	}
}

func TestComputeGitChangeSummaryInspectsOnlyRequestedPath(t *testing.T) {
	workDir, ref := createChangesTestGitRepo(t)
	if err := os.WriteFile(filepath.Join(workDir, "requested.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workDir, "other-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(workDir, "other-link")); err != nil {
		t.Fatal(err)
	}

	change, err := computeGitChangeSummary(workDir, ref, "requested.txt")
	if err != nil {
		t.Fatal(err)
	}
	if change == nil || change.Path != "requested.txt" {
		t.Fatalf("expected only requested path, got %#v", change)
	}
}

func TestLoadGitDiffForPathRejectsConcurrentMutation(t *testing.T) {
	workDir, ref := createChangesTestGitRepo(t)
	path := filepath.Join(workDir, "requested.txt")
	if err := os.WriteFile(path, []byte("first change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err := computeGitChangeSummary(workDir, ref, "requested.txt")
	if err != nil || change == nil {
		t.Fatalf("summary failed: %#v, %v", change, err)
	}

	_, err = loadGitDiffForPath(workDir, ref, "requested.txt", change.Revision, func(_, _, _ string) ([]DiffHunk, error) {
		if writeErr := os.WriteFile(path, []byte("mutated during read\n"), 0o644); writeErr != nil {
			return nil, writeErr
		}
		return []DiffHunk{{NewStart: 1, NewLines: 1}}, nil
	})
	if !errors.Is(err, errChangeRevisionConflict) {
		t.Fatalf("expected concurrent mutation conflict, got %v", err)
	}
}

func TestComputeSnapshotDiffRevisionMatchesReturnedContent(t *testing.T) {
	workDir := t.TempDir()
	content := []byte("coherent content\n")
	if err := os.WriteFile(filepath.Join(workDir, "notes.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	change, ok := computeSnapshotDiffForPath(workDir, map[string]string{"notes.txt": "old"}, "notes.txt")
	if !ok {
		t.Fatal("expected changed snapshot path")
	}
	sum := sha256.Sum256(content)
	if change.Revision != fmt.Sprintf("%x", sum[:]) {
		t.Fatalf("revision was not derived from returned content: %q", change.Revision)
	}
	if len(change.Hunks) != 1 || change.Hunks[0].Lines[0].Content != "coherent content" {
		t.Fatalf("unexpected diff content: %#v", change.Hunks)
	}
}

func createChangesTestGitRepo(t *testing.T) (string, string) {
	t.Helper()
	workDir := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Test User"}, {"config", "user.email", "test@example.com"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(workDir, "requested.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", filepath.Join(workDir, "other-link")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "requested.txt", "other-link"}, {"commit", "-m", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return workDir, strings.TrimSpace(string(output))
}
