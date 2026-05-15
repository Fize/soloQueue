package config

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// ─── MergeTOML ────────────────────────────────────────────────────────────────

func TestMergeTOML_PartialOverride(t *testing.T) {
	base := Settings{
		Session: SessionConfig{TimelineMaxFileMB: 50},
		Log:     LogConfig{Level: "info", Console: true},
	}
	patch := `[log]
level = "debug"
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if result.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug", result.Log.Level)
	}
	if !result.Log.Console {
		t.Errorf("log.console = false, want true (preserved)")
	}
	if result.Session.TimelineMaxFileMB != 50 {
		t.Errorf("session.timelineMaxFileMB = %d, want 50 (preserved)", result.Session.TimelineMaxFileMB)
	}
}

func TestMergeTOML_DeepNestedObject(t *testing.T) {
	base := Settings{
		Log: LogConfig{
			Level:   "info",
			Console: true,
			File:    true,
		},
	}
	patch := `[log]
level = "debug"
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if result.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug", result.Log.Level)
	}
	if !result.Log.Console {
		t.Errorf("log.console = false, want true (preserved)")
	}
}

func TestMergeTOML_EmbeddingNestedMerge(t *testing.T) {
	base := DefaultSettings()
	patch := `[embedding]
enabled = true
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if !result.Embedding.Enabled {
		t.Errorf("embedding.enabled not applied")
	}
	if len(result.Embedding.Providers) != len(base.Embedding.Providers) {
		t.Errorf("embedding.providers len = %d, want preserved (%d)",
			len(result.Embedding.Providers), len(base.Embedding.Providers))
	}
	if len(result.Embedding.Models) != len(base.Embedding.Models) {
		t.Errorf("embedding.models not preserved")
	}
}

func TestMergeTOML_ArrayReplacement(t *testing.T) {
	base := DefaultSettings()
	patch := `[[providers]]
id = "openai"
name = "OpenAI"
enabled = true
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if len(result.Providers) != 1 {
		t.Errorf("providers len = %d, want 1 (wholesale replace)", len(result.Providers))
	}
	if result.Providers[0].ID != "openai" {
		t.Errorf("providers[0].id = %q, want openai", result.Providers[0].ID)
	}
}

func TestMergeTOML_NullPreservesValue(t *testing.T) {
	base := Settings{Log: LogConfig{Level: "info"}}
	// TOML doesn't have null, omit the key to preserve
	patch := `[log]
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if result.Log.Level != "info" {
		t.Errorf("omitted key should preserve, got %q", result.Log.Level)
	}
}

func TestMergeTOML_EmptyPatch_NoOp(t *testing.T) {
	base := DefaultSettings()
	result, err := MergeTOML(base, []byte(``))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if result.Log.Level != base.Log.Level {
		t.Errorf("empty patch should preserve defaults")
	}
}

func TestMergeTOML_InvalidTOML_Errors(t *testing.T) {
	base := DefaultSettings()
	_, err := MergeTOML(base, []byte(`not valid toml`))
	if err == nil {
		t.Error("invalid TOML should return error")
	}
}

func TestMergeTOML_UnknownFields_Ignored(t *testing.T) {
	base := Settings{Log: LogConfig{Level: "info"}}
	patch := `[log]
level = "debug"
unknownField = "xxx"
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if result.Log.Level != "debug" {
		t.Errorf("level = %q, want debug", result.Log.Level)
	}
}

func TestMergeTOML_NumericTypes(t *testing.T) {
	base := Settings{Session: SessionConfig{TimelineMaxFileMB: 100}}
	patch := `[session]
timeline_max_file_mb = 7200
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if result.Session.TimelineMaxFileMB != 7200 {
		t.Errorf("timelineMaxFileMB = %d, want 7200", result.Session.TimelineMaxFileMB)
	}
}

func TestMergeTOML_BooleanOverride(t *testing.T) {
	base := Settings{Log: LogConfig{File: true}}
	patch := `[log]
file = false
`
	result, err := MergeTOML(base, []byte(patch))
	if err != nil {
		t.Fatalf("MergeTOML: %v", err)
	}
	if result.Log.File {
		t.Errorf("file should be overridden to false")
	}
}

// ─── Loader: Load ────────────────────────────────────────────────────────────

func TestLoader_Load_NoFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")

	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loader.Get()
	if got.Log.Level != "info" {
		t.Errorf("log.level = %q, want info", got.Log.Level)
	}
}

func TestLoader_Load_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	writeTOML(t, path, map[string]any{"log": map[string]any{"level": "debug"}})

	loader, _ := NewLoader(DefaultSettings(), path)
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loader.Get()
	if got.Log.Level != "debug" {
		t.Errorf("level = %q, want debug", got.Log.Level)
	}
	if !got.Log.Console {
		t.Errorf("log.console should be preserved from defaults (true)")
	}
}

func TestLoader_Load_InvalidTOML_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	if err := os.WriteFile(path, []byte(`not valid toml`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loader, _ := NewLoader(DefaultSettings(), path)
	err := loader.Load()
	if err == nil {
		t.Fatal("Load should return error for invalid TOML")
	}
}

func TestLoader_Load_PermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	if err := os.WriteFile(path, []byte(`{}`), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}

	loader, _ := NewLoader(DefaultSettings(), path)
	err := loader.Load()
	if err == nil {
		t.Error("Load should fail on permission denied")
	}
}

func TestLoader_Load_MultiLayer(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "settings.toml")
	local := filepath.Join(dir, "settings.local.toml")

	writeTOML(t, main, map[string]any{"log": map[string]any{"level": "debug"}})
	writeTOML(t, local, map[string]any{"log": map[string]any{"console": false}})

	loader, _ := NewLoader(DefaultSettings(), main, local)
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := loader.Get()
	if got.Log.Level != "debug" {
		t.Errorf("level = %q, want debug (from main)", got.Log.Level)
	}
	if got.Log.Console != false {
		t.Errorf("console = %v, want false (from local)", got.Log.Console)
	}
}

func TestLoader_Load_LocalOverridesMain(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "settings.toml")
	local := filepath.Join(dir, "settings.local.toml")

	writeTOML(t, main, map[string]any{"log": map[string]any{"level": "debug"}})
	writeTOML(t, local, map[string]any{"log": map[string]any{"level": "warn"}})

	loader, _ := NewLoader(DefaultSettings(), main, local)
	_ = loader.Load()

	if loader.Get().Log.Level != "warn" {
		t.Errorf("local should override main, got %q", loader.Get().Log.Level)
	}
}

func TestLoader_Load_MissingLocalFile_OK(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "settings.toml")
	local := filepath.Join(dir, "settings.local.toml")
	writeTOML(t, main, map[string]any{"log": map[string]any{"level": "debug"}})

	loader, _ := NewLoader(DefaultSettings(), main, local)
	if err := loader.Load(); err != nil {
		t.Fatalf("missing local should not error, got: %v", err)
	}
	if loader.Get().Log.Level != "debug" {
		t.Errorf("main level not applied")
	}
}



func TestLoader_Save_WritesCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	if err := loader.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("main file should exist: %v", err)
	}
}

func TestLoader_Save_Atomic_OverridesExistingTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()
	if err := loader.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf(".tmp should be renamed away, err: %v", err)
	}
}

func TestLoader_Save_NoPaths_Errors(t *testing.T) {
	_, err := NewLoader(DefaultSettings())
	if err == nil {
		t.Error("NewLoader with no paths should error")
	}
}

// ─── Loader: Concurrency ─────────────────────────────────────────────────────

func TestLoader_ConcurrentGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := loader.Get()
			_ = s.Log.Level
		}()
	}
	wg.Wait()
}

func TestLoader_ConcurrentLoadAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	writeTOML(t, path, map[string]any{"log": map[string]any{"level": "debug"}})
	loader, _ := NewLoader(DefaultSettings(), path)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = loader.Load()
		}()
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = loader.Get()
		}()
	}
	wg.Wait()
}

// ─── expandPath ──────────────────────────────────────────────────────────────

func TestExpandPath_Tilde(t *testing.T) {
	got, err := expandPath("~/.soloqueue/settings.toml")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".soloqueue/settings.toml")
	if got != want {
		t.Errorf("expandPath = %q, want %q", got, want)
	}
}

func TestExpandPath_NoTilde(t *testing.T) {
	got, err := expandPath("/absolute/path")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	if got != "/absolute/path" {
		t.Errorf("expandPath = %q, want unchanged", got)
	}
}

func TestExpandPath_Empty(t *testing.T) {
	got, err := expandPath("")
	if err != nil {
		t.Fatalf("expandPath empty: %v", err)
	}
	if got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
}

func TestExpandPath_RelativePath(t *testing.T) {
	got, err := expandPath("./foo/bar")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	if got != "./foo/bar" {
		t.Errorf("relative path = %q, want unchanged", got)
	}
}

// ─── GlobalService ───────────────────────────────────────────────────────────

func TestGlobalService_DefaultProvider(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = svc.Load()

	p := svc.DefaultProvider()
	if p == nil {
		t.Fatal("DefaultProvider returned nil")
	}
	if p.ID != "deepseek" {
		t.Errorf("id = %q, want deepseek", p.ID)
	}
}

func TestGlobalService_DefaultModelByRole_DefaultsIncludeProMax(t *testing.T) {
	// 验证 DefaultSettings() 中包含 deepseek-v4-pro-max 模型
	s := DefaultSettings()
	found := false
	for _, m := range s.Models {
		if m.ID == "deepseek-v4-pro-max" {
			found = true
			if m.APIModel != "deepseek-v4-pro" {
				t.Errorf("deepseek-v4-pro-max apiModel = %q, want deepseek-v4-pro", m.APIModel)
			}
			if m.Thinking.ReasoningEffort != "max" {
				t.Errorf("deepseek-v4-pro-max reasoningEffort = %q, want max", m.Thinking.ReasoningEffort)
			}
		}
	}
	if !found {
		t.Error("DefaultSettings should include deepseek-v4-pro-max model")
	}

	// 验证 DefaultModels 默认值
	if s.DefaultModels.Expert != "deepseek:deepseek-v4-pro-max" {
		t.Errorf("defaultModels.expert = %q, want deepseek:deepseek-v4-pro-max", s.DefaultModels.Expert)
	}
	if s.DefaultModels.Fallback != "" {
		t.Errorf("defaultModels.fallback = %q, want empty", s.DefaultModels.Fallback)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func writeTOML(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := toml.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeTOMLFile(t *testing.T, path string, v any) {
	t.Helper()
	writeTOML(t, path, v)
}
