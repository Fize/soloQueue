package config

import (
	"os"
	"path/filepath"
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
