package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTP_FileHandlers_ToggleCheckbox(t *testing.T) {
	tempDir := t.TempDir()

	// Create Mux
	mux := NewMux(tempDir, nil)
	defer mux.Close()

	// Test POST /api/files/toggle-checkbox
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
		req := newLocalhostRequest("POST", "/api/files/toggle-checkbox", bytes.NewReader(data))
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
		req = newLocalhostRequest("POST", "/api/files/toggle-checkbox", bytes.NewReader(data))
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

func TestHTTPFileContentAllowsLegacyDesignOutputRasterImage(t *testing.T) {
	workDir := t.TempDir()
	mux := NewMux(workDir, nil)
	defer mux.Close()
	dir := filepath.Join(workDir, "design_output", "travel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan.png")
	if err := os.WriteFile(path, []byte("png data"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := newLocalhostRequest(http.MethodGet, "/api/files/content?path="+path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "png data" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHTTPFileContentRejectsLegacyDesignOutputHTML(t *testing.T) {
	workDir := t.TempDir()
	mux := NewMux(workDir, nil)
	defer mux.Close()
	dir := filepath.Join(workDir, "design_output")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "report.html")
	if err := os.WriteFile(path, []byte("<script>alert(1)</script>"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := newLocalhostRequest(http.MethodGet, "/api/files/content?path="+path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHTTPFileContentRejectsLegacyDesignOutputRootSymlinkEscape(t *testing.T) {
	workDir := t.TempDir()
	externalDir := t.TempDir()
	path := filepath.Join(externalDir, "secret.png")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalDir, filepath.Join(workDir, "design_output")); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(workDir, nil)
	defer mux.Close()

	req := newLocalhostRequest(http.MethodGet, "/api/files/content?path="+filepath.Join(workDir, "design_output", "secret.png"), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}
