package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"

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
		s.ModelRoutes = ModelRoutesConfig{Fallback: "p:fallback-model"}
	})
	if err != nil {
		t.Fatal(err)
	}
	model, usedFallback, err := svc.ResolveScheduledTaskModel(tasktype.Engineering)
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "fallback-model" || !usedFallback {
		t.Fatalf("unexpected resolution: model=%+v fallback=%v", model, usedFallback)
	}
}

func TestResolveScheduledTaskModelUsesTaskModel(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "p", Enabled: true}}
		s.Models = []LLMModel{{ID: "engineering-model", ProviderID: "p", Enabled: true}}
		s.ModelRoutes = ModelRoutesConfig{Engineering: "p:engineering-model"}
	})
	if err != nil {
		t.Fatal(err)
	}
	model, usedFallback, err := svc.ResolveScheduledTaskModel(tasktype.Engineering)
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "engineering-model" || usedFallback {
		t.Fatalf("unexpected resolution: model=%+v fallback=%v", model, usedFallback)
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
		s.ModelRoutes = ModelRoutesConfig{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResolveScheduledTaskModel(tasktype.Engineering); err == nil {
		t.Fatal("expected resolution error without engineering or fallback model")
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

func TestLoader_Watch_InvalidYAMLReportsErrorAndKeepsLastValidSettings(t *testing.T) {
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
	if err := loader.Watch(); err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer loader.StopWatch()

	reloadErrors := make(chan error, 1)
	loader.SetOnError(func(err error) {
		select {
		case reloadErrors <- err:
		default:
		}
	})
	changed := make(chan struct{}, 1)
	loader.SetOnChange(func() error {
		select {
		case changed <- struct{}{}:
		default:
		}
		return nil
	})

	if err := os.WriteFile(path, []byte("log: ["), 0o644); err != nil {
		t.Fatalf("write invalid YAML: %v", err)
	}

	select {
	case err := <-reloadErrors:
		if !strings.Contains(err.Error(), "parse "+path) {
			t.Fatalf("reload error = %q, want parse error for %s", err, path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reload error not reported")
	}

	if got := loader.Get().Log.Level; got != "debug" {
		t.Fatalf("log level after rejected reload = %q, want debug", got)
	}
	select {
	case <-changed:
		t.Fatal("onChange called after rejected reload")
	case <-time.After(200 * time.Millisecond):
	}

	writeYAML(t, path, map[string]any{
		"log": map[string]any{"level": "error"},
	})
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("onChange not called after valid YAML restored")
	}
	if got := loader.Get().Log.Level; got != "error" {
		t.Fatalf("log level after valid reload = %q, want error", got)
	}
}

func TestGlobalService_Watch_InvalidYAMLLogsReloadFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{
		"log": map[string]any{"level": "info"},
	})

	service, err := New(dir)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := service.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	log, err := logger.New(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer log.Close()
	service.SetLogger(log)
	if err := service.Watch(); err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer service.StopWatch()

	if err := os.WriteFile(path, []byte("log: ["), 0o644); err != nil {
		t.Fatalf("write invalid YAML: %v", err)
	}

	logPath := filepath.Join(dir, "logs", "system", "config-"+time.Now().Format("2006-01-02")+".jsonl")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(logPath)
		if readErr == nil && strings.Contains(string(data), "config hot-reload failed") &&
			strings.Contains(string(data), "parse "+path) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("reload failure was not written to %s", logPath)
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
