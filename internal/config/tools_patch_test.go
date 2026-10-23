package config

import (
	"sync"
	"testing"
)

func TestPatchToolsPreservesIndependentUpdatesAndExplicitEmptyValues(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "test-placeholder"
	count := 4321
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := s.PatchTools(ToolsPatch{TavilyAPIKey: &key}); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.PatchTools(ToolsPatch{MaxMatches: &count}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got := s.Get().Tools
	if got.TavilyAPIKey != key || got.MaxMatches != count {
		t.Fatalf("independent updates lost")
	}
	empty := ""
	no := false
	list := []string{}
	got, err = s.PatchTools(ToolsPatch{TavilyAPIKey: &empty, HTTPBlockPrivate: &no, HTTPAllowedHosts: &list})
	if err != nil || got.TavilyAPIKey != "" || got.HTTPBlockPrivate || len(got.HTTPAllowedHosts) != 0 || got.MaxMatches != count {
		t.Fatalf("explicit clears or preserved fields incorrect: %v", err)
	}
	reloaded, err := s.LoadFromDisk()
	if err != nil || reloaded.Tools.MaxMatches != count || reloaded.Tools.TavilyAPIKey != "" {
		t.Fatal("patch not persisted")
	}
}

func TestImageModelValidationAndRejectedPatch(t *testing.T) {
	valid := ImageModelConfig{ID: "one", Name: "One", Provider: "tencent", APIBaseHost: "aiart.tencentcloudapi.com", Region: "ap-guangzhou", Enabled: true, IsDefault: true, SecretIdEnv: "IMAGE_ID", SecretKeyEnv: "IMAGE_KEY"}
	for name, models := range map[string][]ImageModelConfig{
		"valid": {valid}, "empty": {}, "draft": {{ID: "draft", Name: "Draft", Provider: "tencent"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateImageModels(models); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, mutate := range []func(*ImageModelConfig){func(m *ImageModelConfig) { m.ID = "" }, func(m *ImageModelConfig) { m.Provider = "other" }, func(m *ImageModelConfig) { m.Enabled = false }, func(m *ImageModelConfig) { m.IsDefault = false }, func(m *ImageModelConfig) { m.SecretKeyEnv = "" }, func(m *ImageModelConfig) { m.APIBaseHost = "https://bad/path" }} {
		m := valid
		mutate(&m)
		models := []ImageModelConfig{m}
		s, err := New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.PatchTools(ToolsPatch{ImageModels: &models}); err == nil {
			t.Fatal("invalid image configuration accepted")
		}
		if len(s.Get().Tools.ImageModels) != 0 {
			t.Fatal("invalid patch mutated settings")
		}
	}
	if ValidateImageModels([]ImageModelConfig{valid, valid}) == nil {
		t.Fatal("duplicate IDs accepted")
	}
}
