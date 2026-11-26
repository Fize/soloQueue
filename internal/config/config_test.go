package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"

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

func writeBytesAtomically(t *testing.T, path string, data []byte) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("replace file: %v", err)
	}
}

func writeYAMLAtomically(t *testing.T, path string, v any) {
	t.Helper()
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("marshal yaml: %v", err)
	}
	writeBytesAtomically(t, path, data)
}

func waitForWatchToSettle(t *testing.T, committed <-chan Settings, acceptedLevel string) {
	t.Helper()
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case got := <-committed:
			if got.Log.Level != acceptedLevel {
				t.Fatalf("unexpected accepted candidate before rejection = %q", got.Log.Level)
			}
		case <-timer.C:
			return
		}
	}
}

func TestSettingsSerializationOmitsSimulation(t *testing.T) {
	settings := DefaultSettings()
	jsonData, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var jsonFields map[string]any
	if err := json.Unmarshal(jsonData, &jsonFields); err != nil {
		t.Fatal(err)
	}
	if _, exists := jsonFields["simulation"]; exists {
		t.Error("JSON settings still expose simulation")
	}
	if _, exists := jsonFields["session"]; !exists {
		t.Error("JSON settings lost session configuration")
	}
	yamlData, err := settings.MarshalYAMLWithComments()
	if err != nil {
		t.Fatal(err)
	}
	var yamlFields map[string]any
	if err := yaml.Unmarshal(yamlData, &yamlFields); err != nil {
		t.Fatal(err)
	}
	if _, exists := yamlFields["simulation"]; exists {
		t.Error("YAML settings still expose simulation")
	}
	if _, exists := yamlFields["session"]; !exists {
		t.Error("YAML settings lost session configuration")
	}
}

func TestInit_IgnoresLegacySimulationSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{
		"log": map[string]any{"level": "debug"},
		"simulation": map[string]any{
			"db_path":           filepath.Join(dir, "simulation.db"),
			"enable_reflection": true,
			"simulated_hours":   168,
		},
	})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Init(dir)
	if err != nil {
		t.Fatalf("init with legacy simulation settings: %v", err)
	}
	if cfg.Get().Log.Level != "debug" {
		t.Fatal("legacy section prevented loading ordinary settings")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("init rewrote the user's existing settings file")
	}
	data, err := json.Marshal(cfg.Get())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["simulation"]; exists {
		t.Fatal("legacy simulation settings leaked into current configuration")
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

func TestInit_CreatesDefaultConfigWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatalf("init: %v", err)
	}

	path := filepath.Join(dir, "settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}

	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal generated config: %v", err)
	}
	if settings.Log.Level != DefaultSettings().Log.Level {
		t.Errorf("generated log level = %q, want %q", settings.Log.Level, DefaultSettings().Log.Level)
	}
}

func TestInit_PreservesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{
		"log": map[string]any{"level": "debug"},
	})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original config: %v", err)
	}

	if _, err := Init(dir); err != nil {
		t.Fatalf("init: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read existing config: %v", err)
	}
	if string(data) != string(before) {
		t.Fatal("existing config was rewritten during init")
	}
	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal existing config: %v", err)
	}
	if settings.Log.Level != "debug" {
		t.Errorf("existing log level = %q, want debug", settings.Log.Level)
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

func TestDefaultModelUsesConfiguredDefault(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "p", Enabled: true}}
		s.Models = []LLMModel{{ID: "chosen-model", ProviderID: "p", Enabled: true}}
		s.ModelRoutes = ModelRoutesConfig{Default: "p:chosen-model"}
	})
	if err != nil {
		t.Fatal(err)
	}
	model := svc.DefaultModel()
	if model == nil || model.ID != "chosen-model" {
		t.Fatalf("unexpected resolution: %+v", model)
	}
}

func TestDefaultModelReadsLegacyFallbackKey(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "p", Enabled: true}}
		s.Models = []LLMModel{{ID: "legacy-model", ProviderID: "p", Enabled: true}}
		s.ModelRoutes = ModelRoutesConfig{Fallback: "p:legacy-model"}
	})
	if err != nil {
		t.Fatal(err)
	}
	model := svc.DefaultModel()
	if model == nil || model.ID != "legacy-model" {
		t.Fatalf("unexpected resolution: %+v", model)
	}
}

func TestDefaultModelFailsWithoutEnabledModel(t *testing.T) {
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
	if model := svc.DefaultModel(); model != nil {
		t.Fatalf("expected nil model, got %+v", model)
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

func TestLoaderReadsLegacyFallbackModelRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("model_routes:\n  fallback: p:legacy-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}
	svc := &GlobalService{Loader: loader}
	_, err = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "p", Enabled: true}}
		s.Models = []LLMModel{{ID: "legacy-model", ProviderID: "p", Enabled: true}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if model := svc.DefaultModel(); model == nil || model.ID != "legacy-model" {
		t.Fatalf("legacy fallback not used: %+v", model)
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

	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(level string) {
			defer wg.Done()
			_, err := loader.Set(func(s *Settings) {
				s.Log.Level = level
			})
			errCh <- err
		}(string(rune('a' + i)))
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent set: %v", err)
		}
	}

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
	loader.SetOnCommitted(func(Settings) {
		select {
		case called <- struct{}{}:
		default:
		}
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

func TestLoader_Watch_WaitsForInPlaceWriteToSettle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{
		"log": map[string]any{"level": "info"},
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

	committed := make(chan Settings, 2)
	loader.SetOnCommitted(func(settings Settings) {
		committed <- settings
	})

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	if _, err := file.WriteString("log:\n  level: "); err != nil {
		t.Fatalf("write partial settings: %v", err)
	}
	select {
	case settings := <-committed:
		t.Fatalf("committed partial settings with log level %q", settings.Log.Level)
	case <-time.After(20 * time.Millisecond):
	}
	if _, err := file.WriteString("debug\n"); err != nil {
		t.Fatalf("finish settings: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close settings: %v", err)
	}

	select {
	case settings := <-committed:
		if settings.Log.Level != "debug" {
			t.Fatalf("committed log level = %q, want debug", settings.Log.Level)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onCommitted not called after in-place write settled")
	}
}

func TestLoader_Watch_RepeatedStartStopIsRaceFree(t *testing.T) {
	dir := t.TempDir()
	loader, err := NewLoader(DefaultSettings(), filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}

	for range 100 {
		if err := loader.Watch(); err != nil {
			t.Fatalf("watch: %v", err)
		}
		runtime.Gosched()
		loader.StopWatch()
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
	loader.SetOnChange(func(Settings) error {
		select {
		case changed <- struct{}{}:
		default:
		}
		return nil
	})

	writeBytesAtomically(t, path, []byte("log: ["))

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

	writeYAMLAtomically(t, path, map[string]any{
		"log": map[string]any{"level": "error"},
	})
	deadline := time.After(2 * time.Second)
	for {
		if got := loader.Get().Log.Level; got == "error" {
			break
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatalf("onChange not called after valid YAML restored; log level = %q", loader.Get().Log.Level)
		}
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
		if readErr == nil {
			decoder := json.NewDecoder(strings.NewReader(string(data)))
			for {
				var record struct {
					Message string `json:"msg"`
					Error   string `json:"err"`
				}
				if err := decoder.Decode(&record); err != nil {
					break
				}
				if record.Message == "config hot-reload failed" && strings.Contains(record.Error, "parse "+path) {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("reload failure was not written to %s", logPath)
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

func TestSettingsSerializationDoesNotExposeProgressWatchdog(t *testing.T) {
	settings := DefaultSettings()
	jsonData, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonData), "progressWatchdog") {
		t.Fatalf("settings JSON exposes internal watchdog policy: %s", jsonData)
	}
	yamlData, err := yaml.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(yamlData), "progress_watchdog") {
		t.Fatalf("settings YAML exposes internal watchdog policy: %s", yamlData)
	}
}

func TestLoadPreservesExplicitProviderTimeout(t *testing.T) {
	workDir := t.TempDir()
	custom := []byte("providers:\n  - id: custom\n    name: Custom\n    base_url: https://example.invalid/v1\n    enabled: true\n    timeout_ms: 600000\n")
	if err := os.WriteFile(filepath.Join(workDir, "settings.yaml"), custom, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	providers := cfg.Get().Providers
	if len(providers) != 1 || providers[0].TimeoutMs != 600000 {
		t.Fatalf("custom provider timeout = %+v, want explicit timeout_ms=600000 preserved", providers)
	}
}

func TestLoaderApplyReloadCandidateIsInvisibleUntilCallbackAccepts(t *testing.T) {
	loader, err := NewLoader(DefaultSettings(), filepath.Join(t.TempDir(), "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	previous := loader.Get()
	previous.Log.Level = "info"
	loader.current = previous
	candidate := previous
	candidate.Log.Level = "debug"

	callbackStarted := make(chan Settings, 1)
	releaseCallback := make(chan struct{})
	loader.SetOnChange(func(got Settings) error {
		callbackStarted <- got
		<-releaseCallback
		return errors.New("reject candidate")
	})

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- loader.applyReloadCandidate(candidate) }()
	select {
	case got := <-callbackStarted:
		if got.Log.Level != "debug" {
			t.Fatalf("callback candidate log level = %q, want debug", got.Log.Level)
		}
	case <-time.After(time.Second):
		t.Fatal("callback did not start")
	}

	for range 1_000 {
		if got := loader.Get().Log.Level; got != "info" {
			t.Fatalf("Get exposed uncommitted candidate = %q, want info", got)
		}
		runtime.Gosched()
	}
	close(releaseCallback)
	if err := <-reloadDone; err == nil || !strings.Contains(err.Error(), "reject candidate") {
		t.Fatalf("reload error = %v, want rejection", err)
	}
	if got := loader.Get().Log.Level; got != "info" {
		t.Fatalf("rejected candidate committed = %q, want info", got)
	}

	loader.SetOnChange(func(got Settings) error {
		if got.Log.Level != "debug" {
			t.Fatalf("accepted callback candidate = %q, want debug", got.Log.Level)
		}
		if current := loader.Get().Log.Level; current != "info" {
			t.Fatalf("Get during accepted callback = %q, want previous info", current)
		}
		return nil
	})
	if err := loader.applyReloadCandidate(candidate); err != nil {
		t.Fatalf("accept candidate: %v", err)
	}
	if got := loader.Get().Log.Level; got != "debug" {
		t.Fatalf("accepted candidate not committed = %q, want debug", got)
	}
}

func TestLoaderWatchPublishesAcceptedCandidateBeforePostCommitNotification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeYAML(t, path, map[string]any{"log": map[string]any{"level": "info"}})
	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}
	loader.SetOnChange(func(candidate Settings) error {
		if candidate.Log.Level == "invalid" {
			return errors.New("reject invalid candidate")
		}
		return nil
	})
	committed := make(chan Settings, 2)
	loader.SetOnCommitted(func(candidate Settings) {
		if got := loader.Get().Log.Level; got != candidate.Log.Level {
			t.Errorf("post-commit Get log level = %q, want accepted candidate %q", got, candidate.Log.Level)
		}
		committed <- candidate
	})
	errorsSeen := make(chan error, 1)
	loader.SetOnError(func(err error) { errorsSeen <- err })
	if err := loader.Watch(); err != nil {
		t.Fatal(err)
	}
	defer loader.StopWatch()

	writeYAMLAtomically(t, path, map[string]any{"log": map[string]any{"level": "debug"}})
	select {
	case got := <-committed:
		if got.Log.Level != "debug" {
			t.Fatalf("post-commit candidate = %q, want debug", got.Log.Level)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("accepted candidate did not trigger post-commit notification")
	}
	waitForWatchToSettle(t, committed, "debug")

	writeYAMLAtomically(t, path, map[string]any{"log": map[string]any{"level": "invalid"}})
	select {
	case <-errorsSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("rejected candidate error was not reported")
	}
	select {
	case got := <-committed:
		t.Fatalf("rejected candidate triggered post-commit notification: %q", got.Log.Level)
	case <-time.After(200 * time.Millisecond):
	}
	if got := loader.Get().Log.Level; got != "debug" {
		t.Fatalf("rejected candidate was published: %q", got)
	}
}

func TestLoaderSetPublishesAcceptedCandidateBeforePostCommitNotification(t *testing.T) {
	dir := t.TempDir()
	loader, err := NewLoader(DefaultSettings(), filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}
	committed := make(chan Settings, 1)
	loader.SetOnCommitted(func(candidate Settings) {
		if got := loader.Get().Log.Level; got != candidate.Log.Level {
			t.Errorf("programmatic post-commit Get log level = %q, want %q", got, candidate.Log.Level)
		}
		committed <- candidate
	})

	got, err := loader.Set(func(settings *Settings) {
		settings.Log.Level = "debug"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Log.Level != "debug" {
		t.Fatalf("Set returned log level = %q, want debug", got.Log.Level)
	}
	select {
	case candidate := <-committed:
		if candidate.Log.Level != "debug" {
			t.Fatalf("programmatic post-commit candidate = %q, want debug", candidate.Log.Level)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("programmatic Set did not trigger post-commit notification")
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

func TestCapabilitiesSurviveReload(t *testing.T) {
	dir := t.TempDir()
	cfg, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.Set(func(s *Settings) {
		s.Models = []LLMModel{{ID: "test", ProviderID: "provider", Enabled: true,
			Thinking: ThinkingConfig{ReasoningEfforts: []string{"low", "high"}}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	got := reloaded.Get().Models
	if len(got) != 1 || strings.Join(got[0].Thinking.ReasoningEfforts, ",") != "low,high" {
		t.Fatalf("reloaded models: %+v", got)
	}
	if got[0].Thinking.ReasoningEffort != "" {
		t.Fatalf("legacy field resurrected: %q", got[0].Thinking.ReasoningEffort)
	}
}

// A legacy single-value reasoning_effort expands to the full vocabulary on load
// and is cleared, so the new capability set is the only stored shape.
func TestThinkingLegacyReasoningEffortMigratesToCapabilitySet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	legacy := "models:\n" +
		"  - id: legacy-thinking\n" +
		"    provider_id: p\n" +
		"    enabled: true\n" +
		"    thinking:\n" +
		"      enabled: true\n" +
		"      reasoning_effort: high\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	got := cfg.Get().Models
	if len(got) != 1 {
		t.Fatalf("models = %+v", got)
	}
	if got[0].Thinking.ReasoningEffort != "" {
		t.Fatalf("legacy field not cleared: %q", got[0].Thinking.ReasoningEffort)
	}
	if strings.Join(got[0].Thinking.ReasoningEfforts, ",") != "low,medium,high,xhigh,max" {
		t.Fatalf("migrated efforts = %v", got[0].Thinking.ReasoningEfforts)
	}

	// The legacy key never reaches disk again.
	if _, err := cfg.Set(func(st *Settings) { st.Log.Level = "debug" }); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "reasoning_effort:") {
		t.Fatalf("legacy key persisted: %s", saved)
	}
	if !strings.Contains(string(saved), "reasoning_efforts:") {
		t.Fatalf("capability set missing: %s", saved)
	}
}

// Capabilities are normalized on load: lowercase, trimmed, deduped, order kept,
// unknown values dropped.
func TestThinkingCapabilitiesAreNormalizedOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	raw := "models:\n" +
		"  - id: thinking\n" +
		"    provider_id: p\n" +
		"    enabled: true\n" +
		"    thinking:\n" +
		"      enabled: true\n" +
		"      reasoning_efforts: [HIGH, \" xhigh \", ultra, high, max]\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	got := cfg.Get().Models[0].Thinking.ReasoningEfforts
	if strings.Join(got, ",") != "high,xhigh,max" {
		t.Fatalf("normalized efforts = %v", got)
	}
}

// The removed thinking.enabled switch is ignored on load, so a legacy file that
// still carries it keeps the capabilities it declares instead of being muted.
func TestThinkingLegacyEnabledKeyIsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	raw := "models:\n" +
		"  - id: plain\n" +
		"    provider_id: p\n" +
		"    enabled: true\n" +
		"    thinking:\n" +
		"      enabled: false\n" +
		"      reasoning_efforts: [low, high]\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Get().Models[0].Thinking.ReasoningEfforts; strings.Join(got, ",") != "low,high" {
		t.Fatalf("declared capabilities lost: %v", got)
	}
}

// A model that can never think declares nothing: "off" on its own is not a
// capability set.
func TestThinkingOffAloneClearsCapabilities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	raw := "models:\n" +
		"  - id: only-off\n" +
		"    provider_id: p\n" +
		"    enabled: true\n" +
		"    thinking:\n" +
		"      reasoning_efforts: [off]\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Get().Models[0].Thinking.ReasoningEfforts; len(got) != 0 {
		t.Fatalf("off-only model kept capabilities: %v", got)
	}
}

func TestThinkingCapable(t *testing.T) {
	cases := []struct {
		efforts []string
		want    bool
	}{
		{nil, false},
		{[]string{OffReasoningEffort}, false},
		{[]string{OffReasoningEffort, "low"}, true},
		{[]string{"low"}, true},
	}
	for _, tc := range cases {
		if got := ThinkingCapable(tc.efforts); got != tc.want {
			t.Errorf("ThinkingCapable(%v) = %v, want %v", tc.efforts, got, tc.want)
		}
	}
}

func TestCreateModelValidatesThinkingCapabilities(t *testing.T) {
	cfg, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thinking := func(efforts ...string) LLMModel {
		return LLMModel{ID: "m", ProviderID: "p", Enabled: true,
			Thinking: ThinkingConfig{ReasoningEfforts: efforts}}
	}

	if err := cfg.CreateModel(thinking("low", "ultra")); err == nil {
		t.Fatal("expected an unknown reasoning effort to be rejected")
	}
	if err := cfg.CreateModel(thinking("OFF", "low", "high")); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}
	if got := cfg.ModelByID("m").Thinking.ReasoningEfforts; strings.Join(got, ",") != "off,low,high" {
		t.Fatalf("stored efforts = %v", got)
	}

	updated := thinking("medium", "nope")
	if err := cfg.UpdateModel("p", "m", updated); err == nil {
		t.Fatal("expected UpdateModel to reject an unknown reasoning effort")
	}
	if err := cfg.UpdateModel("p", "m", thinking("medium", "max")); err != nil {
		t.Fatalf("UpdateModel: %v", err)
	}
	if got := cfg.ModelByID("m").Thinking.ReasoningEfforts; strings.Join(got, ",") != "medium,max" {
		t.Fatalf("updated efforts = %v", got)
	}
}

// The built-in thinking models declare the intensity vocabulary. "off" stays
// undeclared: disabling thinking is provider-specific and must be an explicit
// per-model decision.
func TestDefaultThinkingModelsDeclareCapabilitySet(t *testing.T) {
	for _, model := range DefaultSettings().Models {
		if model.Thinking.ReasoningEffort != "" {
			t.Fatalf("%s still sets a legacy reasoning effort", model.ID)
		}
		if len(model.Thinking.ReasoningEfforts) == 0 {
			continue
		}
		if strings.Join(model.Thinking.ReasoningEfforts, ",") != "low,medium,high,xhigh,max" {
			t.Fatalf("%s efforts = %v", model.ID, model.Thinking.ReasoningEfforts)
		}
	}
}
