package config

import (
	"context"
	"errors"
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

// ─── Loader: Set / Save ──────────────────────────────────────────────────────

func TestLoader_Set_WritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	if err := loader.Set(func(s *Settings) { s.Log.Level = "debug" }); err != nil {
		t.Fatalf("Set: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var saved Settings
	_ = toml.Unmarshal(data, &saved)
	if saved.Log.Level != "debug" {
		t.Errorf("saved level = %q", saved.Log.Level)
	}
}

func TestLoader_Set_AtomicWrite_NoTmpLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	_ = loader.Set(func(s *Settings) { s.Log.Level = "debug" })

	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf(".tmp should not exist after successful Set, err: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("main file missing: %v", err)
	}
}

func TestLoader_Set_WriteFails_RollsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("readonly dir semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	readonly := filepath.Join(dir, "ro")
	if err := os.Mkdir(readonly, 0o555); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	defer os.Chmod(readonly, 0o755)

	path := filepath.Join(readonly, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	originalLevel := loader.Get().Log.Level

	err := loader.Set(func(s *Settings) { s.Log.Level = "debug" })
	if err == nil {
		t.Fatal("Set should fail in readonly dir")
	}

	if loader.Get().Log.Level != originalLevel {
		t.Errorf("after failed Set, current = %q, want %q (rollback)",
			loader.Get().Log.Level, originalLevel)
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

func TestLoader_ConcurrentGetAndSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = loader.Get()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = loader.Set(func(s *Settings) {
					s.Session.TimelineMaxFileMB = 14
				})
			}
		}(i)
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

func TestGlobalService_DefaultProvider_NoDefault_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	_ = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "custom", Name: "Custom", IsDefault: false, Enabled: true}}
	})

	if svc.DefaultProvider() != nil {
		t.Error("DefaultProvider should return nil when no isDefault=true")
	}
}

func TestGlobalService_DefaultEmbeddingModel(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.DefaultEmbeddingModel()
	if m == nil {
		t.Fatal("DefaultEmbeddingModel nil")
	}
	if m.ID != "nomic-embed-text:latest" {
		t.Errorf("id = %q", m.ID)
	}
}

func TestGlobalService_DefaultEmbeddingModel_None(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	_ = svc.Set(func(s *Settings) { s.Embedding.Models = nil })

	if svc.DefaultEmbeddingModel() != nil {
		t.Error("should be nil when no embedding models")
	}
}

func TestGlobalService_ProviderByID(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	if p := svc.ProviderByID("deepseek"); p == nil || p.ID != "deepseek" {
		t.Errorf("ProviderByID(deepseek) = %v", p)
	}
	if p := svc.ProviderByID("nonexistent"); p != nil {
		t.Errorf("unknown id should be nil, got %v", p)
	}
}

func TestGlobalService_ModelByID(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.ModelByID("deepseek-v4-pro")
	if m == nil {
		t.Fatal("ModelByID nil")
	}
	if !m.Thinking.Enabled {
		t.Error("deepseek-v4-pro should have thinking.enabled=true")
	}
	if m.Thinking.ReasoningEffort != "high" {
		t.Errorf("reasoningEffort = %q", m.Thinking.ReasoningEffort)
	}
}

func TestGlobalService_ModelByID_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	if m := svc.ModelByID("nonexistent"); m != nil {
		t.Errorf("unknown model id should be nil, got %v", m)
	}
}

func TestGlobalService_Set_Persists(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	_ = svc.Set(func(s *Settings) { s.Log.Level = "debug" })

	svc2, _ := New(dir)
	_ = svc2.Load()
	if svc2.Get().Log.Level != "debug" {
		t.Errorf("persisted log.level = %q", svc2.Get().Log.Level)
	}
}

func TestGlobalService_LocalOverride(t *testing.T) {
	dir := t.TempDir()
	writeTOMLFile(t, filepath.Join(dir, "settings.local.toml"),
		map[string]any{"log": map[string]any{"level": "debug"}})

	svc, _ := New(dir)
	_ = svc.Load()

	if svc.Get().Log.Level != "debug" {
		t.Errorf("local override not applied, got %q", svc.Get().Log.Level)
	}
}

func TestGlobalService_ReturnedPointers_Independent(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	p1 := svc.ProviderByID("deepseek")
	p1.Name = "MUTATED"

	p2 := svc.ProviderByID("deepseek")
	if p2.Name == "MUTATED" {
		t.Errorf("mutating returned pointer leaked into loader state")
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

// ─── New API tests ──────────────────────────────────────────────────────────

func TestGlobalService_EmbedsLoader(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := svc.Load(); err != nil {
		t.Fatalf("Load (via embed): %v", err)
	}
	settings := svc.Get()
	if settings.Log.Level != "info" {
		t.Errorf("embedded Get().Log.Level = %q, want info", settings.Log.Level)
	}
	if err := svc.Set(func(s *Settings) { s.Log.Level = "debug" }); err != nil {
		t.Fatalf("Set (via embed): %v", err)
	}
	if svc.Get().Log.Level != "debug" {
		t.Error("Set via embed did not persist in-memory")
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save (via embed): %v", err)
	}

	var _ *Loader[Settings] = svc.Loader
}

func TestNewLoaderValidation_NoPaths(t *testing.T) {
	_, err := NewLoader(DefaultSettings())
	if err == nil {
		t.Error("NewLoader with no paths should error")
	}
}

func TestNewLoaderValidation_EmptyPath(t *testing.T) {
	_, err := NewLoader(DefaultSettings(), "/valid/path", "")
	if err == nil {
		t.Error("NewLoader with empty path should error")
	}
}

func TestNewLoaderValidation_DuplicatePath(t *testing.T) {
	_, err := NewLoader(DefaultSettings(), "/same/path", "/same/path")
	if err == nil {
		t.Error("NewLoader with duplicate paths should error")
	}
}

func TestLoader_LoadContext_CancelledCtx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := loader.LoadContext(ctx)
	if err == nil {
		t.Fatal("LoadContext with cancelled ctx should error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should wrap context.Canceled, got: %v", err)
	}
}

func TestLoader_SaveContext_CancelledCtx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := loader.SaveContext(ctx)
	if err == nil {
		t.Fatal("SaveContext with cancelled ctx should error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should wrap context.Canceled, got: %v", err)
	}
}

// ─── parseProviderModelID ────────────────────────────────────────────────────

func TestParseProviderModelID(t *testing.T) {
	tests := []struct {
		input   string
		wantPID string
		wantMID string
		wantOK  bool
	}{
		{"deepseek:deepseek-v4-pro", "deepseek", "deepseek-v4-pro", true},
		{"openai:gpt-4o", "openai", "gpt-4o", true},
		{"", "", "", false},
		{"nocolon", "", "", false},
		{":emptyprovider", "", "", false},
		{"emptyid:", "", "", false},
		{"a:b:c", "a", "b:c", true}, // SplitN(2): only first colon splits
	}

	for _, tt := range tests {
		pid, mid, ok := parseProviderModelID(tt.input)
		if ok != tt.wantOK || pid != tt.wantPID || mid != tt.wantMID {
			t.Errorf("parseProviderModelID(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.input, pid, mid, ok, tt.wantPID, tt.wantMID, tt.wantOK)
		}
	}
}

// ─── ModelByProviderID ────────────────────────────────────────────────────────

func TestGlobalService_ModelByProviderID(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.ModelByProviderID("deepseek", "deepseek-v4-pro")
	if m == nil {
		t.Fatal("ModelByProviderID(deepseek, deepseek-v4-pro) nil")
	}
	if m.Thinking.ReasoningEffort != "high" {
		t.Errorf("reasoningEffort = %q, want high", m.Thinking.ReasoningEffort)
	}
}

func TestGlobalService_ModelByProviderID_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	if m := svc.ModelByProviderID("deepseek", "nonexistent"); m != nil {
		t.Errorf("nonexistent model should be nil, got %v", m)
	}
	if m := svc.ModelByProviderID("nonexistent", "deepseek-v4-pro"); m != nil {
		t.Errorf("nonexistent provider should be nil, got %v", m)
	}
}

// ─── DefaultModelByRole ────────────────────────────────────────────────────────

func TestGlobalService_DefaultModelByRole_Defaults(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	// With default config, all roles can resolve to corresponding models
	expert := svc.DefaultModelByRole("expert")
	if expert == nil {
		t.Fatal("expert model nil")
	}
	if expert.ID != "deepseek-v4-pro-max" {
		t.Errorf("expert = %q, want deepseek-v4-pro-max", expert.ID)
	}
	if expert.Thinking.ReasoningEffort != "max" {
		t.Errorf("expert reasoningEffort = %q, want max", expert.Thinking.ReasoningEffort)
	}

	superior := svc.DefaultModelByRole("superior")
	if superior == nil {
		t.Fatal("superior model nil")
	}
	if superior.ID != "deepseek-v4-flash-thinking-max" {
		t.Errorf("superior = %q, want deepseek-v4-pro", superior.ID)
	}

	universal := svc.DefaultModelByRole("universal")
	if universal == nil {
		t.Fatal("universal model nil")
	}
	if universal.ID != "deepseek-v4-flash-thinking" {
		t.Errorf("universal = %q, want deepseek-v4-flash-thinking", universal.ID)
	}

	fast := svc.DefaultModelByRole("fast")
	if fast == nil {
		t.Fatal("fast model nil")
	}
	if fast.ID != "deepseek-v4-flash" {
		t.Errorf("fast = %q, want deepseek-v4-flash", fast.ID)
	}
	if fast.Thinking.Enabled {
		t.Error("fast model should have thinking disabled")
	}
}

func TestGlobalService_DefaultModelByRole_UserOverride(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	// User overrides expert role to deepseek-v4-pro (high reasoning)
	_ = svc.Set(func(s *Settings) {
		s.DefaultModels.Expert = "deepseek:deepseek-v4-pro"
	})

	expert := svc.DefaultModelByRole("expert")
	if expert == nil {
		t.Fatal("expert model nil after override")
	}
	if expert.ID != "deepseek-v4-pro" {
		t.Errorf("expert = %q, want deepseek-v4-pro after override", expert.ID)
	}
	// After user config, effort follows model definition, not max
	if expert.Thinking.ReasoningEffort != "high" {
		t.Errorf("expert reasoningEffort = %q, want high (from model definition)", expert.Thinking.ReasoningEffort)
	}
}

func TestGlobalService_DefaultModelByRole_Fallback(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	// Clear expert config, set fallback
	_ = svc.Set(func(s *Settings) {
		s.DefaultModels.Expert = ""
		s.DefaultModels.Fallback = "deepseek:deepseek-v4-flash"
	})

	expert := svc.DefaultModelByRole("expert")
	if expert == nil {
		t.Fatal("expert model nil with fallback")
	}
	if expert.ID != "deepseek-v4-flash" {
		t.Errorf("expert = %q, want deepseek-v4-flash (from fallback)", expert.ID)
	}
}

func TestGlobalService_DefaultModelByRole_NoFallback_UsesHardcoded(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	// Clear expert and fallback, should use hardcoded default
	_ = svc.Set(func(s *Settings) {
		s.DefaultModels.Expert = ""
		s.DefaultModels.Fallback = ""
	})

	expert := svc.DefaultModelByRole("expert")
	if expert == nil {
		t.Fatal("expert should use hardcoded default")
	}
	if expert.ID != "deepseek-v4-pro-max" {
		t.Errorf("expert = %q, want deepseek-v4-pro-max (hardcoded default)", expert.ID)
	}
}

func TestGlobalService_DefaultModelByRole_UnknownRole(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	if m := svc.DefaultModelByRole("unknown"); m != nil {
		t.Errorf("unknown role should return nil, got %v", m)
	}
}

func TestGlobalService_DefaultModelByRole_InvalidFormat(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	// 配置值不是 provider:id 格式
	_ = svc.Set(func(s *Settings) {
		s.DefaultModels.Expert = "invalid-format"
		s.DefaultModels.Fallback = ""
	})

	if m := svc.DefaultModelByRole("expert"); m != nil {
		t.Errorf("invalid format should return nil, got %v", m)
	}
}

func TestGlobalService_DefaultModelByRole_NonexistentModel(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	// provider:id 格式正确但模型不存在
	_ = svc.Set(func(s *Settings) {
		s.DefaultModels.Expert = "deepseek:nonexistent-model"
		s.DefaultModels.Fallback = ""
	})

	if m := svc.DefaultModelByRole("expert"); m != nil {
		t.Errorf("nonexistent model should return nil, got %v", m)
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
