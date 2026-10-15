package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPListFilesSupportsConditionalRequests(t *testing.T) {
	workDir := t.TempDir()
	planDir := filepath.Join(workDir, "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(planDir, "plan.md")
	modTime := time.Date(2026, 10, 15, 12, 0, 0, 123456789, time.UTC)
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	mux := NewMux(workDir, nil)
	defer mux.Close()
	requestURL := "/api/files/list?dir=" + url.QueryEscape(planDir)

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, newLocalhostRequest(http.MethodGet, requestURL, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if strings.HasPrefix(etag, "W/") {
		t.Fatalf("directory representation ETag must remain strong, got %q", etag)
	}
	var files []FileInfo
	if err := json.Unmarshal(first.Body.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files=%+v", files)
	}
	parsedModTime, err := time.Parse(time.RFC3339Nano, files[0].ModTime)
	if err != nil {
		t.Fatalf("modTime=%q: %v", files[0].ModTime, err)
	}
	if !parsedModTime.Equal(modTime) || parsedModTime.Nanosecond() != 123456789 {
		t.Fatalf("files=%+v", files)
	}

	conditionalRequest := newLocalhostRequest(http.MethodGet, requestURL, nil)
	conditionalRequest.Header.Set("If-None-Match", etag)
	unchanged := httptest.NewRecorder()
	mux.ServeHTTP(unchanged, conditionalRequest)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged status=%d body=%q", unchanged.Code, unchanged.Body.String())
	}
	if unchanged.Header().Get("ETag") != etag {
		t.Fatalf("unchanged ETag=%q want %q", unchanged.Header().Get("ETag"), etag)
	}

	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	changedRequest := newLocalhostRequest(http.MethodGet, requestURL, nil)
	changedRequest.Header.Set("If-None-Match", etag)
	changed := httptest.NewRecorder()
	mux.ServeHTTP(changed, changedRequest)
	if changed.Code != http.StatusOK {
		t.Fatalf("changed status=%d body=%q", changed.Code, changed.Body.String())
	}
	if changed.Header().Get("ETag") == "" || changed.Header().Get("ETag") == etag {
		t.Fatalf("changed ETag=%q old=%q", changed.Header().Get("ETag"), etag)
	}
}

func TestHTTPFileContentSupportsConditionalRequests(t *testing.T) {
	workDir := t.TempDir()
	planDir := filepath.Join(workDir, "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(planDir, "plan.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstModTime := time.Date(2026, 10, 15, 12, 0, 0, 1, time.UTC)
	if err := os.Chtimes(path, firstModTime, firstModTime); err != nil {
		t.Fatal(err)
	}

	mux := NewMux(workDir, nil)
	defer mux.Close()
	requestURL := "/api/files/content?path=" + url.QueryEscape(path)

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, newLocalhostRequest(http.MethodGet, requestURL, nil))
	if first.Code != http.StatusOK || first.Body.String() != "first" {
		t.Fatalf("first status=%d body=%q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if !strings.HasPrefix(etag, `W/"`) {
		t.Fatalf("file metadata ETag must be weak, got %q", etag)
	}

	conditionalRequest := newLocalhostRequest(http.MethodGet, requestURL, nil)
	conditionalRequest.Header.Set("If-None-Match", etag)
	unchanged := httptest.NewRecorder()
	mux.ServeHTTP(unchanged, conditionalRequest)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged status=%d body=%q", unchanged.Code, unchanged.Body.String())
	}
	if unchanged.Header().Get("ETag") != etag {
		t.Fatalf("unchanged ETag=%q want %q", unchanged.Header().Get("ETag"), etag)
	}

	strongCandidate := strings.TrimPrefix(etag, "W/")
	strongRequest := newLocalhostRequest(http.MethodGet, requestURL, nil)
	strongRequest.Header.Set("If-None-Match", strongCandidate)
	strongMatch := httptest.NewRecorder()
	mux.ServeHTTP(strongMatch, strongRequest)
	if strongMatch.Code != http.StatusNotModified {
		t.Fatalf("strong candidate must weak-match metadata ETag: status=%d", strongMatch.Code)
	}

	if err := os.WriteFile(path, []byte("later"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondModTime := firstModTime.Add(time.Nanosecond)
	if err := os.Chtimes(path, secondModTime, secondModTime); err != nil {
		t.Fatal(err)
	}
	changedRequest := newLocalhostRequest(http.MethodGet, requestURL, nil)
	changedRequest.Header.Set("If-None-Match", etag)
	changed := httptest.NewRecorder()
	mux.ServeHTTP(changed, changedRequest)
	if changed.Code != http.StatusOK || changed.Body.String() != "later" {
		t.Fatalf("changed status=%d body=%q", changed.Code, changed.Body.String())
	}
	if changed.Header().Get("ETag") == "" || changed.Header().Get("ETag") == etag {
		t.Fatalf("changed ETag=%q old=%q", changed.Header().Get("ETag"), etag)
	}
}

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

		// Non-standard [/] is plain text, so Task 3 is checkbox index 1.
		body = map[string]any{
			"path":  planFile,
			"index": 1,
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
		if !strings.Contains(updatedContent, "- [/] Task 2") {
			t.Errorf("Non-standard marker should remain plain text: %s", updatedContent)
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
