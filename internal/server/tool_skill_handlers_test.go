package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/skill"
)

func TestHTTP_SkillManagementFlow(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Set up simulated Store catalog directory
	storeSkillsDir := filepath.Join(tempDir, "store", "skills")
	testSkillDir := filepath.Join(storeSkillsDir, "test-catalog-skill")
	if err := os.MkdirAll(testSkillDir, 0755); err != nil {
		t.Fatalf("failed to create store skill directory: %v", err)
	}

	skillMD := `---
name: "test-catalog-skill"
description: "A test store skill"
triggers:
  - "run test catalog"
---
Instructions for test catalog skill
`
	if err := os.WriteFile(filepath.Join(testSkillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatalf("failed to write store SKILL.md: %v", err)
	}

	// 2. Set up user skills directory
	userSkillsDir := filepath.Join(tempDir, "user", "skills")
	if err := os.MkdirAll(userSkillsDir, 0755); err != nil {
		t.Fatalf("failed to create user skills directory: %v", err)
	}

	// Initialize the skill registry
	reg := skill.NewSkillRegistry()
	dirs := map[string]string{
		"builtin": storeSkillsDir,
		"user":    userSkillsDir,
	}
	if err := reg.Rebuild(dirs); err != nil {
		t.Fatalf("failed to build skill registry: %v", err)
	}

	// Create Server Mux
	mux := NewMux(tempDir, nil, WithSkillRegistry(reg), WithSkillDirs(dirs))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		mux.Close()
	})

	client := srv.Client()

	// Ensure we restore loopback headers to bypass auth
	setLoopbackHeaders := func(req *http.Request) {
		req.Host = "localhost:8765"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
	}

	// ── Test 1: GET /api/skills/store (ListStoreSkills) ──
	{
		req, _ := http.NewRequest("GET", srv.URL+"/api/skills/store", nil)
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /api/skills/store failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		var listResp struct {
			Skills []struct {
				ID          string   `json:"id"`
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Triggers    []string `json:"triggers"`
				Enabled     bool     `json:"enabled"`
			} `json:"skills"`
		}
		if err := json.Unmarshal(body, &listResp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, body: %s", err, body)
		}

		if len(listResp.Skills) != 1 || listResp.Skills[0].ID != "test-catalog-skill" {
			t.Errorf("expected test-catalog-skill in store list, got %+v", listResp.Skills)
		}
	}

	// ── Test 2: POST /api/skills/install (InstallSkill from store) ──
	{
		installBody := map[string]string{
			"source": "store",
			"id":     "test-catalog-skill",
		}
		jsonBytes, _ := json.Marshal(installBody)
		req, _ := http.NewRequest("POST", srv.URL+"/api/skills/install", bytes.NewBuffer(jsonBytes))
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /api/skills/install failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		// Verify file got copied to userSkillsDir
		userSkillMDPath := filepath.Join(userSkillsDir, "test-catalog-skill", "SKILL.md")
		if _, err := os.Stat(userSkillMDPath); os.IsNotExist(err) {
			t.Errorf("expected skill to be copied to user directory, file not found: %s", userSkillMDPath)
		}
	}

	// ── Test 3: GET /api/skills/{id} (GetSkillDetail) ──
	{
		req, _ := http.NewRequest("GET", srv.URL+"/api/skills/test-catalog-skill", nil)
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /api/skills/{id} failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		var detailResp struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Body        string `json:"body"`
		}
		if err := json.Unmarshal(body, &detailResp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, body: %s", err, body)
		}

		if detailResp.ID != "test-catalog-skill" || !strings.Contains(detailResp.Body, "Instructions for test catalog skill") {
			t.Errorf("unexpected detail payload: %+v", detailResp)
		}
	}

	// ── Test 4: PUT /api/skills/{id} (UpdateUserSkill) ──
	{
		updatePayload := map[string]any{
			"description": "Updated description",
			"body":        "Updated instructions content",
			"triggers":    []string{"updated trigger 1", "updated trigger 2"},
		}
		jsonBytes, _ := json.Marshal(updatePayload)
		req, _ := http.NewRequest("PUT", srv.URL+"/api/skills/test-catalog-skill", bytes.NewBuffer(jsonBytes))
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PUT /api/skills/{id} failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		// Re-fetch details and verify updates
		reqDetail, _ := http.NewRequest("GET", srv.URL+"/api/skills/test-catalog-skill", nil)
		setLoopbackHeaders(reqDetail)
		detailResp, err := client.Do(reqDetail)
		if err != nil {
			t.Fatalf("re-fetching detail failed: %v", err)
		}
		defer detailResp.Body.Close()

		body, _ := io.ReadAll(detailResp.Body)
		var detail struct {
			ID          string   `json:"id"`
			Description string   `json:"description"`
			Body        string   `json:"body"`
			Triggers    []string `json:"triggers"`
		}
		json.Unmarshal(body, &detail)

		if strings.TrimSpace(detail.Description) != "Updated description" || !strings.Contains(detail.Body, "Updated instructions content") {
			t.Errorf("skill updates not persisted: description=%q, body=%q, detail=%#v", detail.Description, detail.Body, detail)
		}
		if len(detail.Triggers) != 2 || detail.Triggers[0] != "updated trigger 1" {
			t.Errorf("skill triggers not updated, got %+v", detail.Triggers)
		}
	}

	// ── Test 5: GET /api/skills/{id}/files (ListSkillFiles) ──
	{
		req, _ := http.NewRequest("GET", srv.URL+"/api/skills/test-catalog-skill/files", nil)
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /api/skills/{id}/files failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		var filesResp struct {
			Files []struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			} `json:"files"`
		}
		if err := json.Unmarshal(body, &filesResp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, body: %s", err, body)
		}

		if len(filesResp.Files) != 1 || filesResp.Files[0].Path != "SKILL.md" {
			t.Errorf("expected list containing only SKILL.md, got %+v", filesResp.Files)
		}
	}

	// ── Test 6: POST /api/skills/{id}/toggle (ToggleSkill) ──
	{
		// 1. Toggle to disabled (creates .disabled)
		reqToggle, _ := http.NewRequest("POST", srv.URL+"/api/skills/test-catalog-skill/toggle", nil)
		setLoopbackHeaders(reqToggle)
		resp, err := client.Do(reqToggle)
		if err != nil {
			t.Fatalf("POST /api/skills/{id}/toggle failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		disabledFile := filepath.Join(userSkillsDir, "test-catalog-skill", ".disabled")
		if _, err := os.Stat(disabledFile); os.IsNotExist(err) {
			t.Errorf("expected .disabled file to exist after disabling, not found")
		}

		// 2. Toggle to enabled (removes .disabled)
		reqToggle2, _ := http.NewRequest("POST", srv.URL+"/api/skills/test-catalog-skill/toggle", nil)
		setLoopbackHeaders(reqToggle2)
		resp2, err := client.Do(reqToggle2)
		if err != nil {
			t.Fatalf("POST /api/skills/{id}/toggle second call failed: %v", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp2.StatusCode)
		}

		if _, err := os.Stat(disabledFile); err == nil {
			t.Errorf("expected .disabled file to be deleted after enabling, but it exists")
		}
	}

	// ── Test 7: POST /api/skills (ImportSkill) ──
	{
		importPayload := map[string]any{
			"name":        "new-imported-skill",
			"description": "An imported user skill",
			"body":        "Instructions for imported skill",
			"triggers":    []string{"imported action"},
		}
		jsonBytes, _ := json.Marshal(importPayload)
		req, _ := http.NewRequest("POST", srv.URL+"/api/skills", bytes.NewBuffer(jsonBytes))
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /api/skills failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("expected 201 Created, got %d", resp.StatusCode)
		}

		userSkillMDPath := filepath.Join(userSkillsDir, "new-imported-skill", "SKILL.md")
		if _, err := os.Stat(userSkillMDPath); os.IsNotExist(err) {
			t.Errorf("expected imported skill to exist in user directory, file not found: %s", userSkillMDPath)
		}
	}

	// ── Test 8: DELETE /api/skills/{id} (Delete/Uninstall Skill) ──
	{
		req, _ := http.NewRequest("DELETE", srv.URL+"/api/skills/test-catalog-skill", nil)
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("DELETE /api/skills/{id} failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		// Verify user skill directory is deleted
		deletedDir := filepath.Join(userSkillsDir, "test-catalog-skill")
		if _, err := os.Stat(deletedDir); err == nil {
			t.Errorf("expected skill directory %s to be deleted, but it still exists", deletedDir)
		}
	}

	// ── Test 9: POST /api/skills/{id}/auto-update (ToggleAutoUpdate) ──
	{
		autoUpdatePayload := map[string]any{
			"enabled": true,
		}
		jsonBytes, _ := json.Marshal(autoUpdatePayload)
		req, _ := http.NewRequest("POST", srv.URL+"/api/skills/new-imported-skill/auto-update", bytes.NewBuffer(jsonBytes))
		setLoopbackHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /api/skills/{id}/auto-update failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		var autoUpdateResp struct {
			ID         string `json:"id"`
			AutoUpdate bool   `json:"auto_update"`
		}
		if err := json.Unmarshal(body, &autoUpdateResp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, body: %s", err, body)
		}

		if autoUpdateResp.ID != "new-imported-skill" || !autoUpdateResp.AutoUpdate {
			t.Errorf("unexpected auto-update response: %+v", autoUpdateResp)
		}

		// Verify state is persisted to skills_update.toml in tempDir
		tomlPath := filepath.Join(tempDir, "skills_update.toml")
		if _, err := os.Stat(tomlPath); err != nil {
			t.Errorf("expected skills_update.toml to exist, got: %v", err)
		}
	}
}
