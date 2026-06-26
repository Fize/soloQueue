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
