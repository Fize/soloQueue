package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
)

func TestHTTP_InstalledSkillsAreReadOnly(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "directory-name")
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: catalog-id
description: An installed skill
metadata:
  openclaw:
    requires:
      env: [TEST_SKILL_TOKEN]
---
Use this installed skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "guide.md"), []byte("guide"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := skill.NewSkillRegistry()
	dirs := map[string]string{"user": filepath.Join(root, "skills")}
	if err := reg.Rebuild(dirs); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(root, nil, WithSkillRegistry(reg), WithSkillDirs(dirs))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close(); mux.Close() })

	get := func(path string) (*http.Response, []byte) {
		t.Helper()
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return resp, body
	}

	resp, body := get("/api/skills")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/skills status = %d, body = %s", resp.StatusCode, body)
	}
	var list SkillListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Skills[0].ID != "catalog-id" {
		t.Fatalf("unexpected installed skill list: %+v", list)
	}
	if list.Skills[0].RequiredEnv[0] != "TEST_SKILL_TOKEN" {
		t.Fatalf("openclaw metadata was not loaded: %+v", list.Skills[0])
	}

	resp, body = get("/api/skills/catalog-id")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET detail status = %d, body = %s", resp.StatusCode, body)
	}
	var detail SkillInfoResponse
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Body != "Use this installed skill." {
		t.Fatalf("unexpected detail body: %q", detail.Body)
	}

	resp, body = get("/api/skills/catalog-id/files")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET files status = %d, body = %s", resp.StatusCode, body)
	}
	var files struct {
		Files []skill.SkillFileEntry `json:"files"`
	}
	if err := json.Unmarshal(body, &files); err != nil {
		t.Fatal(err)
	}
	if len(files.Files) != 3 {
		t.Fatalf("unexpected installed skill files: %+v", files.Files)
	}

	for _, methodPath := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/skills/store"},
		{http.MethodPost, "/api/skills/install"},
		{http.MethodPost, "/api/skills/catalog-id/toggle"},
		{http.MethodPut, "/api/skills/catalog-id"},
		{http.MethodDelete, "/api/skills/catalog-id"},
	} {
		req, err := http.NewRequest(methodPath.method, srv.URL+methodPath.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got.Body.Close()
		if got.StatusCode != http.StatusNotFound && got.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s status = %d, want 404 or 405", methodPath.method, methodPath.path, got.StatusCode)
		}
	}
}
