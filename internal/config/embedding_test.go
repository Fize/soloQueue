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
