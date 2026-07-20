package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func writeYAML(t *testing.T, path string, v any) {
	t.Helper()
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("marshal yaml: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
}

func TestLoader_Load_NoFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	loader, err := NewLoader(DefaultSettings(), filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	if err := loader.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	settings := loader.Get()
	if settings.Log.Level != "info" {
		t.Errorf("log level = %q, want info", settings.Log.Level)
	}
}

func TestLoader_Load_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{
		"log": map[string]any{"level": "debug"},
	})

	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	if err := loader.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	settings := loader.Get()
	if settings.Log.Level != "debug" {
		t.Errorf("log level = %q, want debug", settings.Log.Level)
	}
}

func TestLoader_Load_InvalidYAML_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("not valid yaml: :"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	if err := loader.Load(); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoader_Save_WritesCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	_ = loader.Load()
	if _, err := loader.Set(func(s *Settings) {
		s.Log.Level = "error"
	}); err != nil {
		t.Fatalf("set: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal saved: %v", err)
	}
	if settings.Log.Level != "error" {
		t.Errorf("saved log level = %q, want error", settings.Log.Level)
	}
}

func TestResolveScheduledTaskModelUsesConfiguredFallback(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "p", Enabled: true}}
		s.Models = []LLMModel{{ID: "fallback-model", ProviderID: "p", Enabled: true}}
		s.DefaultModels = DefaultModelsConfig{Fallback: "p:fallback-model"}
	})
	if err != nil {
		t.Fatal(err)
	}
	model, role, usedFallback, err := svc.ResolveScheduledTaskModel("L2")
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "fallback-model" || role != "superior" || !usedFallback {
		t.Fatalf("unexpected resolution: model=%+v role=%q fallback=%v", model, role, usedFallback)
	}
}

func TestResolveScheduledTaskModelSupportsL4Apex(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "p", Enabled: true}}
		s.Models = []LLMModel{{ID: "apex-model", ProviderID: "p", Enabled: true}}
		s.DefaultModels = DefaultModelsConfig{Apex: "p:apex-model"}
	})
	if err != nil {
		t.Fatal(err)
	}
	model, role, usedFallback, err := svc.ResolveScheduledTaskModel("L4")
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "apex-model" || role != "apex" || usedFallback {
		t.Fatalf("unexpected L4 resolution: model=%+v role=%q fallback=%v", model, role, usedFallback)
	}
}

func TestResolveScheduledTaskModelFailsWithoutConfiguredFallback(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Set(func(s *Settings) {
		s.Providers = nil
		s.Models = nil
		s.DefaultModels = DefaultModelsConfig{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.ResolveScheduledTaskModel("L2"); err == nil {
		t.Fatal("expected resolution error without superior or fallback model")
	}
}

func TestLoader_Save_WritesWechatBots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatal(err)
	}
	_ = loader.Load()
	if _, err := loader.Set(func(s *Settings) {
		s.WechatBots = []WechatBotConfig{{ID: "personal", Enabled: true, BotToken: "token", BotID: "bot"}}
	}); err != nil {
		t.Fatal(err)
	}

	var saved Settings
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.WechatBots) != 1 || saved.WechatBots[0].BotToken != "token" {
		t.Fatalf("saved WeChat config = %#v", saved.WechatBots)
	}
}

func TestSettingsLoadsLegacyWeixinBots(t *testing.T) {
	var settings Settings
	if err := yaml.Unmarshal([]byte("weixin_bots:\n  - id: legacy\n    bot_token: secret\n"), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.WechatBots) != 1 || settings.WechatBots[0].ID != "legacy" || settings.WechatBots[0].BotToken != "secret" {
		t.Fatalf("legacy WeChat config = %#v", settings.WechatBots)
	}
}

func TestWechatCredentialsAreNotSerializedToJSON(t *testing.T) {
	data, err := json.Marshal(Settings{WechatBots: []WechatBotConfig{{ID: "personal", BotToken: "secret-token", BotID: "secret-id"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "secret-id") {
		t.Fatalf("credentials leaked in JSON: %s", data)
	}
}

func TestLoader_Set_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	_ = loader.Load()

	for i := 0; i < 10; i++ {
		go func(level string) {
			loader.Set(func(s *Settings) {
				s.Log.Level = level
			})
		}(string(rune('a' + i)))
	}
	time.Sleep(100 * time.Millisecond)

	settings := loader.Get()
	if settings.Log.Level == "" {
		t.Error("log level should be set")
	}
}

func TestLoader_Watch_ReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	_ = loader.Load()
	if err := loader.Watch(); err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer loader.StopWatch()

	called := make(chan struct{}, 1)
	loader.SetOnChange(func() error {
		select {
		case called <- struct{}{}:
		default:
		}
		return nil
	})

	writeYAML(t, path, map[string]any{
		"log": map[string]any{"level": "debug"},
	})

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("onChange not called")
	}

	settings := loader.Get()
	if settings.Log.Level != "debug" {
		t.Errorf("after reload log level = %q, want debug", settings.Log.Level)
	}
}

func TestLoader_ReadFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{
		"auth": map[string]any{"user": "alice"},
	})

	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	settings, err := loader.ReadFromDisk()
	if err != nil {
		t.Fatalf("read from disk: %v", err)
	}
	if settings.Auth.User != "alice" {
		t.Errorf("auth user = %q, want alice", settings.Auth.User)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	got, err := expandPath("~/.soloqueue/settings.yaml")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	want := filepath.Join(home, ".soloqueue/settings.yaml")
	if got != want {
		t.Errorf("expandPath = %q, want %q", got, want)
	}
}

func TestSpeechConfigDefaults(t *testing.T) {
	settings := DefaultSettings()
	if settings.Speech.Enabled {
		t.Error("Speech.Enabled should default to false")
	}
	if settings.Speech.Model != "small" {
		t.Errorf("Speech.Model = %q, want small", settings.Speech.Model)
	}
	if settings.Speech.ModelDir != "" {
		t.Errorf("Speech.ModelDir = %q, want empty", settings.Speech.ModelDir)
	}
}

func TestSpeechConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{
		"speech": map[string]any{
			"enabled":   true,
			"model":     "base",
			"model_dir": "/custom/models",
		},
	})

	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	if err := loader.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	settings := loader.Get()
	if !settings.Speech.Enabled {
		t.Error("Speech.Enabled should be true")
	}
	if settings.Speech.Model != "base" {
		t.Errorf("Speech.Model = %q, want base", settings.Speech.Model)
	}
	if settings.Speech.ModelDir != "/custom/models" {
		t.Errorf("Speech.ModelDir = %q, want /custom/models", settings.Speech.ModelDir)
	}
}
