package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
)

func TestDeleteProviderCascadesModelsAndReportsCount(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Set(func(settings *config.Settings) {
		settings.Providers = []config.LLMProvider{
			{ID: "used", Enabled: true, IsDefault: true},
			{ID: "backup", Enabled: true},
		}
		settings.Models = []config.LLMModel{
			{ID: "first", ProviderID: "used", Enabled: true},
			{ID: "second", ProviderID: "used", Enabled: true},
			{ID: "fallback", ProviderID: "backup", Enabled: true},
		}
		settings.ModelRoutes = config.ModelRoutesConfig{Default: "used:first"}
	}); err != nil {
		t.Fatal(err)
	}

	mux := NewMux(workDir, nil, WithConfigService(cfg))
	t.Cleanup(func() { _ = mux.Close() })

	recorder := httpRecord(t, mux, http.MethodDelete, "/api/config/providers/used", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Deleted           string   `json:"deleted"`
		DeletedModels     []string `json:"deletedModels"`
		DeletedModelCount int      `json:"deletedModelCount"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Deleted != "used" || response.DeletedModelCount != 2 {
		t.Fatalf("response = %+v", response)
	}
	if len(response.DeletedModels) != 2 || response.DeletedModels[0] != "first" || response.DeletedModels[1] != "second" {
		t.Fatalf("deleted models = %v", response.DeletedModels)
	}

	settings := cfg.Get()
	if len(settings.Providers) != 1 || settings.Providers[0].ID != "backup" {
		t.Fatalf("providers = %+v", settings.Providers)
	}
	if len(settings.Models) != 1 || settings.Models[0].ID != "fallback" {
		t.Fatalf("models = %+v", settings.Models)
	}
	if model := cfg.DefaultModel(); model == nil || model.ProviderID != "backup" || model.ID != "fallback" {
		t.Fatalf("fallback model = %+v", model)
	}
}
