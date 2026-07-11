package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

func TestMigrateIfNeeded_FromDB(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "soloqueue.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	provider := LLMProvider{ID: "openai", Name: "OpenAI", Enabled: true}
	if err := SaveProvider(ctx, db, provider); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	model := LLMModel{ID: "gpt-4", ProviderID: "openai", Enabled: true}
	if err := SaveModel(ctx, db, model); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if err := SaveDefaultModels(ctx, db, DefaultModelsConfig{Expert: "openai:gpt-4"}); err != nil {
		t.Fatalf("save default models: %v", err)
	}

	svc, err := New(dir)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := svc.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := svc.SetDB(db); err != nil {
		t.Fatalf("set db: %v", err)
	}

	snapshot, err := svc.MigrateIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if len(snapshot.Providers) != 1 || snapshot.Providers[0].ID != "openai" {
		t.Errorf("providers = %+v, want [openai]", snapshot.Providers)
	}
	if len(snapshot.Models) != 1 || snapshot.Models[0].ID != "gpt-4" {
		t.Errorf("models = %+v, want [gpt-4]", snapshot.Models)
	}
	if snapshot.DefaultModels.Expert != "openai:gpt-4" {
		t.Errorf("default expert = %q, want openai:gpt-4", snapshot.DefaultModels.Expert)
	}

	yamlPath := filepath.Join(dir, "settings.yaml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Errorf("settings.yaml not created: %v", err)
	}
}

func TestMigrateIfNeeded_FromTOML(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "settings.toml")
	if err := os.WriteFile(tomlPath, []byte("[auth]\nuser = \"admin\"\n"), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}

	svc, err := New(dir)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := svc.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	snapshot, err := svc.MigrateIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if snapshot.Auth.User != "admin" {
		t.Errorf("auth user = %q, want admin", snapshot.Auth.User)
	}

	yamlPath := filepath.Join(dir, "settings.yaml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Errorf("settings.yaml not created: %v", err)
	}
}

func TestMigrateIfNeeded_AlreadyMigrated(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(yamlPath, []byte("log:\n  level: warn\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	svc, err := New(dir)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := svc.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	snapshot, err := svc.MigrateIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if snapshot.Log.Level != "warn" {
		t.Errorf("log level = %q, want warn", snapshot.Log.Level)
	}
}

func TestSetDB_DoesNotSeed(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "soloqueue.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	svc, err := New(dir)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := svc.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := svc.SetDB(db); err != nil {
		t.Fatalf("set db: %v", err)
	}

	settings := svc.Get()
	if len(settings.Providers) != len(DefaultSettings().Providers) {
		t.Errorf("providers should come from defaults, got %d", len(settings.Providers))
	}
}
