package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalEmbeddingValidationBeforePersistence(t *testing.T) {
	dir := t.TempDir()
	cfg, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	valid := cfg.Get().Embedding
	valid.Provider = "onnx"
	valid.Enabled = true
	if err = cfg.UpdateEmbedding(valid); err != nil {
		t.Fatal(err)
	}
	got := cfg.Get().Embedding
	if got.Local.Model != "multilingual-e5-small" || got.Local.Device != "auto" {
		t.Fatal(got.Local)
	}
	before, err := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []LocalEmbeddingConfig{{Model: "unsupported", Device: "auto"}, {Model: "multilingual-e5-small", Device: "cuda"}} {
		candidate := got
		candidate.Local = invalid
		if err := cfg.UpdateEmbedding(candidate); err == nil {
			t.Fatal("accepted unsupported local configuration")
		}
		after, _ := os.ReadFile(filepath.Join(dir, "settings.yaml"))
		if string(after) != string(before) || cfg.Get().Embedding.Local != got.Local {
			t.Fatal("invalid configuration was persisted")
		}
	}
}

func TestPublicEmbeddingDeviceAlwaysAutomatic(t *testing.T) {
	cfg, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	embedding := cfg.Get().Embedding
	embedding.Provider = "onnx"
	embedding.Local.Device = "cpu"
	if err = cfg.UpdateEmbedding(embedding); err != nil {
		t.Fatal(err)
	}
	if cfg.Get().Embedding.Local.Device != "auto" {
		t.Fatal("public settings forced CPU")
	}
}

func TestMemoryModesPersistWithoutNewFields(t *testing.T) {
	if DefaultSettings().Embedding.Enabled {
		t.Fatal("long-term memory must default off")
	}
	for _, mode := range []struct {
		name, provider string
		enabled        bool
	}{{"off", "onnx", false}, {"base", "none", true}, {"local", "onnx", true}, {"remote", "openai", true}} {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			value := cfg.Get().Embedding
			value.Provider = mode.provider
			value.Enabled = mode.enabled
			value.Providers[0].APIKey = "test-preserved-credential"
			if err := cfg.UpdateEmbedding(value); err != nil {
				t.Fatal(err)
			}
			reloaded, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := reloaded.Load(); err != nil {
				t.Fatal(err)
			}
			got := reloaded.Get().Embedding
			if got.Enabled != mode.enabled || got.Provider != mode.provider || got.Local.Model != value.Local.Model || got.Providers[0].APIKey != value.Providers[0].APIKey || got.Models[0].ID != value.Models[0].ID {
				t.Fatal("mode/settings did not survive reload")
			}
		})
	}
}
