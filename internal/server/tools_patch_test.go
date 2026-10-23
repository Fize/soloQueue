package server

import (
	"github.com/xiaobaitu/soloqueue/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolsPatchHTTP(t *testing.T) {
	svc, err := config.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := &Mux{configSvc: svc}
	for _, body := range []string{`null`, `[]`, `{"unknown":true}`, `{"tavilyApiKey":null}`, `{"imageModels":[{"id":"x","name":"X","provider":"tencent","enabled":null}]}`, `{"maxMatches":"bad"}`, `{"maxMatches":1} {}`, `{"imageModels":[{"id":"x","name":"X","provider":"other"}]}`, `{"imageModels":[{"id":"x","name":"X","provider":"tencent","unknown":1}]}`} {
		w := httptest.NewRecorder()
		m.handlePatchToolsConfig(w, httptest.NewRequest("PATCH", "/api/config/tools", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid body accepted: %d", w.Code)
		}
	}
	for _, body := range []string{`{"maxMatches":123}`, `{"tavilyApiKey":"placeholder"}`, `{"tavilyApiKey":"","httpBlockPrivate":false,"imageModels":[]}`} {
		w := httptest.NewRecorder()
		m.handlePatchToolsConfig(w, httptest.NewRequest("PATCH", "/api/config/tools", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("patch failed: %d %s", w.Code, w.Body.String())
		}
	}
	if svc.Get().Tools.MaxMatches != 123 || svc.Get().Tools.TavilyAPIKey != "" || svc.Get().Tools.HTTPBlockPrivate {
		t.Fatal("partial update lost fields or clear failed")
	}
	// Existing full-replacement clients remain supported.
	w := httptest.NewRecorder()
	m.handleUpdateToolsConfig(w, httptest.NewRequest("PUT", "/api/config/tools", strings.NewReader(`{"maxMatches":9}`)))
	if w.Code != 200 || svc.Get().Tools.MaxMatches != 9 {
		t.Fatal("legacy PUT failed")
	}
}
