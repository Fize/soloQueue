package config

import "testing"

func TestResolveDefaultModelPrefersConfiguredRoute(t *testing.T) {
	settings := Settings{
		Providers: []LLMProvider{
			{ID: "first", Enabled: true},
			{ID: "second", Enabled: true},
		},
		Models: []LLMModel{
			{ID: "first-model", ProviderID: "first", Enabled: true},
			{ID: "chosen", ProviderID: "second", Enabled: true},
		},
		ModelRoutes: ModelRoutesConfig{Default: "second:chosen"},
	}

	model := ResolveDefaultModel(settings)
	if model == nil || model.ProviderID != "second" || model.ID != "chosen" {
		t.Fatalf("resolved model = %+v, want second:chosen", model)
	}
}

func TestResolveDefaultModelFallsBackToFirstEnabledModel(t *testing.T) {
	settings := Settings{
		Providers: []LLMProvider{
			{ID: "disabled", Enabled: false},
			{ID: "first-enabled", Enabled: true},
			{ID: "second-enabled", Enabled: true},
		},
		Models: []LLMModel{
			{ID: "disabled-provider-model", ProviderID: "disabled", Enabled: true},
			{ID: "disabled-model", ProviderID: "first-enabled", Enabled: false},
			{ID: "first-enabled-model", ProviderID: "first-enabled", Enabled: true},
			{ID: "second-enabled-model", ProviderID: "second-enabled", Enabled: true},
		},
		ModelRoutes: ModelRoutesConfig{Default: "missing:model"},
	}

	model := ResolveDefaultModel(settings)
	if model == nil || model.ProviderID != "first-enabled" || model.ID != "first-enabled-model" {
		t.Fatalf("resolved model = %+v, want first-enabled:first-enabled-model", model)
	}
}

func TestResolveDefaultModelSkipsEnabledProviderWithoutModels(t *testing.T) {
	settings := Settings{
		Providers: []LLMProvider{
			{ID: "empty", Enabled: true},
			{ID: "usable", Enabled: true},
		},
		Models: []LLMModel{
			{ID: "disabled", ProviderID: "empty", Enabled: false},
			{ID: "available", ProviderID: "usable", Enabled: true},
		},
	}

	model := ResolveDefaultModel(settings)
	if model == nil || model.ProviderID != "usable" || model.ID != "available" {
		t.Fatalf("resolved model = %+v, want usable:available", model)
	}
}

func TestResolveDefaultModelReturnsNilWithoutEnabledModel(t *testing.T) {
	settings := Settings{
		Providers: []LLMProvider{{ID: "provider", Enabled: true}},
		Models:    []LLMModel{{ID: "disabled", ProviderID: "provider", Enabled: false}},
	}

	if model := ResolveDefaultModel(settings); model != nil {
		t.Fatalf("resolved model = %+v, want nil", model)
	}
}

func TestDefaultProviderFallsBackToFirstEnabled(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(func(settings *Settings) {
		settings.Providers = []LLMProvider{
			{ID: "disabled-default", Enabled: false, IsDefault: true},
			{ID: "first-enabled", Enabled: true},
		}
	}); err != nil {
		t.Fatal(err)
	}

	provider := svc.DefaultProvider()
	if provider == nil || provider.ID != "first-enabled" {
		t.Fatalf("default provider = %+v, want first-enabled", provider)
	}
}

func TestDeleteProviderCascadesModelsAndFallsBack(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(func(settings *Settings) {
		settings.Providers = []LLMProvider{
			{ID: "used", Enabled: true, IsDefault: true},
			{ID: "backup", Enabled: true},
		}
		settings.Models = []LLMModel{
			{ID: "first", ProviderID: "used", Enabled: true},
			{ID: "second", ProviderID: "used", Enabled: true},
			{ID: "fallback", ProviderID: "backup", Enabled: true},
		}
		settings.ModelRoutes = ModelRoutesConfig{Default: "used:first"}
	}); err != nil {
		t.Fatal(err)
	}

	deletedModels, err := svc.DeleteProvider("used")
	if err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if len(deletedModels) != 2 || deletedModels[0] != "first" || deletedModels[1] != "second" {
		t.Fatalf("deleted models = %v, want [first second]", deletedModels)
	}

	settings := svc.Get()
	if len(settings.Providers) != 1 || settings.Providers[0].ID != "backup" {
		t.Fatalf("providers after delete = %+v", settings.Providers)
	}
	if len(settings.Models) != 1 || settings.Models[0].ID != "fallback" {
		t.Fatalf("models after delete = %+v", settings.Models)
	}
	if settings.ModelRoutes.Default != "used:first" {
		t.Fatalf("model route changed to %q, want original stale preference", settings.ModelRoutes.Default)
	}
	if model := svc.DefaultModel(); model == nil || model.ProviderID != "backup" || model.ID != "fallback" {
		t.Fatalf("resolved model = %+v, want backup:fallback", model)
	}

	persisted, err := svc.LoadFromDisk()
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if len(persisted.Providers) != 1 || persisted.Providers[0].ID != "backup" {
		t.Fatalf("persisted providers = %+v", persisted.Providers)
	}
	if len(persisted.Models) != 1 || persisted.Models[0].ID != "fallback" {
		t.Fatalf("persisted models = %+v", persisted.Models)
	}
}

func TestDeleteProviderUnknownDoesNotChangeSettings(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(func(settings *Settings) {
		settings.Providers = []LLMProvider{{ID: "kept", Enabled: true}}
		settings.Models = []LLMModel{{ID: "kept-model", ProviderID: "kept", Enabled: true}}
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.DeleteProvider("missing"); err == nil {
		t.Fatal("DeleteProvider(missing) succeeded, want not-found error")
	}
	settings := svc.Get()
	if len(settings.Providers) != 1 || len(settings.Models) != 1 {
		t.Fatalf("settings changed after failed delete: providers=%+v models=%+v", settings.Providers, settings.Models)
	}
}
