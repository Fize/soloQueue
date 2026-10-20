package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/managed"
)

func TestEmbeddingStatusAndActionsContract(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	manager := engine.NewVectorManager(shared.DB, &shared.WMu, dir, engine.VectorOptions{Prepare: func(ctx context.Context, progress managed.Progress) (managed.Paths, error) {
		<-ctx.Done()
		return managed.Paths{}, ctx.Err()
	}})
	defer manager.Close()
	mux := NewMux(dir, nil, WithConfigService(cfg), WithEmbeddingManager(manager))
	defer mux.Close()
	for _, test := range []struct {
		method, path, body string
		code               int
	}{{"GET", "/api/config/embedding/status", "", 200}, {"POST", "/api/config/embedding/actions", `{"action":"unknown"}`, 400}, {"POST", "/api/config/embedding/actions", `{"action":"retry","extra":true}`, 400}, {"POST", "/api/config/embedding/actions", `{"action":"retry"} {}`, 400}, {"POST", "/api/config/embedding/actions", `{"action":"retry"}`, 409}} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if response.Code != test.code {
			t.Fatal(test, response.Code, response.Body.String())
		}
	}
	manager.Apply(engine.VectorConfig{Provider: "onnx", Enabled: true}, nil, nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/config/embedding/actions", strings.NewReader(`{"action":"cancel"}`)))
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	var status map[string]any
	if err = json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"provider", "state", "model", "device", "backend", "dimension", "downloadedBytes", "totalBytes", "indexed", "pending", "failed"} {
		if _, ok := status[key]; !ok {
			t.Fatal("missing contract field", key)
		}
	}
	if status["state"] != "cancelled" {
		t.Fatal(status)
	}
	absent := NewMux(t.TempDir(), nil)
	defer absent.Close()
	for _, path := range []string{"/api/config/embedding/status", "/api/config/embedding/actions"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "actions") {
			method = http.MethodPost
		}
		response := httptest.NewRecorder()
		absent.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(`{"action":"retry"}`)))
		if response.Code != 503 {
			t.Fatal(path, response.Code)
		}
	}
}
func TestEmbeddingConfigRejectsInvalidLocalBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	initial := cfg.Get().Embedding
	initial.Provider = "none"
	if err = cfg.UpdateEmbedding(initial); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	mux := NewMux(dir, nil, WithConfigService(cfg))
	defer mux.Close()
	for _, body := range []string{`{"provider":"onnx","enabled":true,"local":{"model":"bad","device":"auto"}}`, `{"provider":"onnx","enabled":true,"local":{"model":"multilingual-e5-small","device":"gpu"}}`} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config/embedding", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatal(response.Code, response.Body.String())
		}
		after, _ := os.ReadFile(filepath.Join(dir, "settings.yaml"))
		if string(after) != string(before) {
			t.Fatal("invalid local settings persisted")
		}
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config/embedding", strings.NewReader(`{"provider":"onnx","enabled":true}`)))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if cfg.Get().Embedding.Local.Device != "auto" {
		t.Fatal(cfg.Get().Embedding)
	}
}
