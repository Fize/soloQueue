package server

import (
	"bytes"
	"encoding/json"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/storage"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStoragePolicyPreviewAndRevision(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.Init(root)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMux(root, nil, WithConfigService(cfg))
	defer m.Close()
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		return w
	}
	w := do("GET", "/api/storage", nil)
	if w.Code != 200 {
		t.Fatalf("get %d %s", w.Code, w.Body.String())
	}
	var state struct {
		Policy   config.StorageConfig `json:"policy"`
		Revision string               `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	state.Policy.ExecutionDetails.RetentionDays = 2
	req := map[string]any{"policy": state.Policy, "revision": state.Revision}
	w = do("PUT", "/api/storage", req)
	if w.Code != http.StatusConflict {
		t.Fatalf("tightening bypass %d %s", w.Code, w.Body.String())
	}
	w = do("POST", "/api/storage/preview", req)
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	var preview struct {
		ID string `json:"preview_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	req["preview_id"] = preview.ID
	w = do("PUT", "/api/storage", req)
	if w.Code != 200 {
		t.Fatalf("apply %d %s", w.Code, w.Body.String())
	}
	if cfg.Get().Storage.ExecutionDetails.RetentionDays != 2 {
		t.Fatal("policy not saved")
	}
	w = do("PUT", "/api/storage", req)
	if w.Code != 409 {
		t.Fatalf("stale edit accepted %d", w.Code)
	}
	w = do("POST", "/api/storage/cleanup", map[string]any{"preview_id": preview.ID, "revision": state.Revision, "confirm": true})
	if w.Code != 409 {
		t.Fatal("old policy cleanup accepted")
	}
	w = do("POST", "/api/storage/preview", map[string]any{"revision": state.Revision, "path": "../../"})
	if w.Code != 400 {
		t.Fatal("arbitrary path input accepted")
	}
}

func TestHTTPOverrideDisableRequiresPreview(t *testing.T) {
	for _, field := range []string{"days", "count"} {
		t.Run(field, func(t *testing.T) {
			root := t.TempDir()
			cfg, err := config.Init(root)
			if err != nil {
				t.Fatal(err)
			}
			_, err = cfg.Set(func(s *config.Settings) {
				s.Storage = config.DefaultStorageConfig()
				s.Storage.RuntimeLogs.HTTPOverride = true
				if field == "days" {
					s.Storage.RuntimeLogs.HTTPRetentionDays = 60
				} else {
					s.Storage.RuntimeLogs.HTTPMaxFiles = 60
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			m := NewMux(root, nil, WithConfigService(cfg))
			defer m.Close()
			old := cfg.Get().EffectiveStorage()
			proposed := old
			proposed.RuntimeLogs.HTTPOverride = false
			body, _ := json.Marshal(map[string]any{"policy": proposed, "revision": storage.Revision(old)})
			w := httptest.NewRecorder()
			m.handleStorageUpdate(w, httptest.NewRequest("PUT", "/api/storage", bytes.NewReader(body)))
			if w.Code != 409 {
				t.Fatalf("HTTP override disabling bypassed preview: %d %s", w.Code, w.Body.String())
			}
			preview, err := m.storage.Preview(proposed, storage.Revision(old), false)
			if err != nil {
				t.Fatal(err)
			}
			body, _ = json.Marshal(map[string]any{"policy": proposed, "revision": storage.Revision(old), "preview_id": preview.ID})
			w = httptest.NewRecorder()
			m.handleStorageUpdate(w, httptest.NewRequest("PUT", "/api/storage", bytes.NewReader(body)))
			if w.Code != 200 {
				t.Fatalf("confirmed update: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
