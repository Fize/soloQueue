package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func httpRecord(t *testing.T, m *Mux, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, newLocalhostRequest(method, target, body))
	return recorder
}

func newL2ModelTestMux(t *testing.T) (*Mux, string) {
	t.Helper()
	workDir := t.TempDir()
	svc, err := config.New(workDir)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}
	m := NewMux(workDir, nil, WithConfigService(svc))
	t.Cleanup(func() { _ = m.Close() })

	dir := filepath.Join(workDir, "logs", "timelines", "l2-abc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir timeline: %v", err)
	}
	if err := session.MergeAndSavePath(filepath.Join(dir, session.MetaFileName), func(meta *session.SessionMeta) {
		meta.Group = "engineering"
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	return m, workDir
}

func TestL2SessionModelRoundTripWithoutActivation(t *testing.T) {
	m, workDir := newL2ModelTestMux(t)

	recorder := httpRecord(t, m, http.MethodGet, "/api/session/l2/abc/model", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var empty sessionModelResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if empty.Selection != nil || empty.Source != "default" {
		t.Fatalf("initial = %+v, want auto", empty)
	}
	if empty.Effective == nil || empty.Effective.ModelID == "" {
		t.Fatalf("effective model missing: %+v", empty)
	}

	body := strings.NewReader(`{"provider_id":"deepseek","model_id":"deepseek-v4-flash-thinking","reasoning_effort":"max"}`)
	recorder = httpRecord(t, m, http.MethodPut, "/api/session/l2/abc/model", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var updated sessionModelResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	if updated.Selection == nil || updated.Selection.ModelID != "deepseek-v4-flash-thinking" || updated.Selection.ReasoningEffort != "max" {
		t.Fatalf("PUT selection = %+v", updated.Selection)
	}
	if updated.Source != "user" || updated.Effective == nil || updated.Effective.ModelID != "deepseek-v4-flash" {
		t.Fatalf("PUT effective = %+v", updated)
	}
	// The effective view carries the capability set the model declares.
	if len(updated.Effective.ReasoningEfforts) != 5 || len(updated.Selection.ReasoningEfforts) != 5 {
		t.Fatalf("capabilities missing from response: %+v", updated)
	}

	meta, err := session.LoadMeta(workDir, "abc")
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Model.ModelID != "deepseek-v4-flash-thinking" || meta.Model.ReasoningEffort != "max" {
		t.Fatalf("persisted model = %+v", meta.Model)
	}

	// Resetting to auto clears the persisted selection.
	recorder = httpRecord(t, m, http.MethodPut, "/api/session/l2/abc/model", strings.NewReader(`{"provider_id":"","model_id":""}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("reset status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	meta, err = session.LoadMeta(workDir, "abc")
	if err != nil {
		t.Fatalf("LoadMeta after reset: %v", err)
	}
	if !meta.Model.IsAuto() {
		t.Fatalf("reset persisted %+v", meta.Model)
	}
}

func TestL2SessionModelRejectsUnknownModel(t *testing.T) {
	m, _ := newL2ModelTestMux(t)

	body := strings.NewReader(`{"provider_id":"deepseek","model_id":"does-not-exist"}`)
	recorder := httpRecord(t, m, http.MethodPut, "/api/session/l2/abc/model", body)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestL2SessionModelUnknownSession(t *testing.T) {
	m, _ := newL2ModelTestMux(t)

	recorder := httpRecord(t, m, http.MethodGet, "/api/session/l2/missing/model", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

// A session exists as soon as it is created, but it has no meta.json until its
// first message activates it. Selecting a model in the composer before that
// point must work and survive the activation.
func TestL2SessionModelOnCreatedSession(t *testing.T) {
	workDir := t.TempDir()
	svc, err := config.New(workDir)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}
	store := session.NewL2SessionStore(nil, workDir, nil)
	if _, err := store.Create(context.Background(), "fresh", "engineering", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	m := NewMux(workDir, nil, WithConfigService(svc), WithL2SessionStore(store))
	t.Cleanup(func() { _ = m.Close() })

	recorder := httpRecord(t, m, http.MethodGet, "/api/session/l2/fresh/model", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var initial sessionModelResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if initial.Selection != nil || initial.Source != "default" {
		t.Fatalf("initial = %+v, want auto", initial)
	}

	body := strings.NewReader(`{"provider_id":"deepseek","model_id":"deepseek-v4-flash-thinking","reasoning_effort":"max"}`)
	recorder = httpRecord(t, m, http.MethodPut, "/api/session/l2/fresh/model", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body = %s", recorder.Code, recorder.Body.String())
	}

	// The choice is readable straight away, before any activation.
	sel, ok := store.ModelSelection("fresh")
	if !ok || sel.ModelID != "deepseek-v4-flash-thinking" || sel.ReasoningEffort != "max" {
		t.Fatalf("store selection = %+v ok = %v", sel, ok)
	}

	// meta.json carries the group, otherwise the session would be unrestorable.
	meta, err := session.LoadMeta(workDir, "fresh")
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Group != "engineering" {
		t.Fatalf("meta group = %q, want engineering", meta.Group)
	}
	if meta.Model.ModelID != "deepseek-v4-flash-thinking" || meta.Model.ReasoningEffort != "max" {
		t.Fatalf("persisted model = %+v", meta.Model)
	}

	// An id that neither the store nor the disk knows stays a 404.
	recorder = httpRecord(t, m, http.MethodPut, "/api/session/l2/missing/model", strings.NewReader(`{"provider_id":"","model_id":""}`))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing PUT status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
