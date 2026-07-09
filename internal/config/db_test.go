package config

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

func TestDatabaseSettings_SyncAndReload(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create a GlobalService with default settings and empty settings.toml
	svc, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create GlobalService: %v", err)
	}
	if err := svc.Load(); err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}

	// 2. Open temporary SQLite database
	dbPath := filepath.Join(tmpDir, "entries.db")
	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	// 3. Set the DB, which triggers seedDatabaseIfNeeded and ReloadFromDB
	if err := svc.SetDB(db); err != nil {
		t.Fatalf("SetDB failed: %v", err)
	}

	// 4. Verify system_settings was seeded correctly
	ctx := context.Background()
	var seededTools ToolsConfig
	ok, err := LoadSystemSetting(ctx, db, "tools", &seededTools)
	if err != nil {
		t.Fatalf("Failed to load tools config from DB: %v", err)
	}
	if !ok {
		t.Fatalf("Expected tools config to be seeded in DB, but key was not found")
	}

	// Validate seeded values match default settings
	defaultSettings := DefaultSettings()
	if seededTools.MaxFileSize != defaultSettings.Tools.MaxFileSize {
		t.Errorf("Seeded MaxFileSize = %d, want default %d", seededTools.MaxFileSize, defaultSettings.Tools.MaxFileSize)
	}

	// 5. Modify setting in DB and verify override (no config file fallback)
	seededTools.MaxFileSize = 999999
	if err := SaveSystemSetting(ctx, db, "tools", seededTools); err != nil {
		t.Fatalf("Failed to save updated tools config to DB: %v", err)
	}

	// Reload from DB to refresh cache
	if err := svc.ReloadFromDB(); err != nil {
		t.Fatalf("ReloadFromDB failed: %v", err)
	}

	// Verify svc.Get() returns the database-backed MaxFileSize
	currentSettings := svc.Get()
	if currentSettings.Tools.MaxFileSize != 999999 {
		t.Errorf("svc.Get().Tools.MaxFileSize = %d, expected overridden DB value 999999", currentSettings.Tools.MaxFileSize)
	}

	// Verify svc.LoadFromDisk() also contains the DB override
	diskSettings, err := svc.LoadFromDisk()
	if err != nil {
		t.Fatalf("LoadFromDisk failed: %v", err)
	}
	if diskSettings.Tools.MaxFileSize != 999999 {
		t.Errorf("svc.LoadFromDisk().Tools.MaxFileSize = %d, expected overridden DB value 999999", diskSettings.Tools.MaxFileSize)
	}
}

func TestQQBotsMigrationAndPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Open SQLite DB first
	dbPath := filepath.Join(tmpDir, "entries.db")
	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create system_settings table manually to seed legacy qqbot before service init
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS system_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("Failed to create system_settings table: %v", err)
	}

	// 2. Seed legacy single "qqbot" config
	legacyJSON := `{"enabled":true,"appId":"12345","appSecret":"secret_val","intents":1024,"sandbox":true}`
	if _, err := db.ExecContext(ctx, `INSERT INTO system_settings (key, value) VALUES ('qqbot', ?)`, legacyJSON); err != nil {
		t.Fatalf("Failed to insert legacy qqbot config: %v", err)
	}

	// 3. Create GlobalService
	svc, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create GlobalService: %v", err)
	}
	if err := svc.Load(); err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}

	// 4. Set the DB, which should trigger the migration from 'qqbot' to 'qqbots'
	if err := svc.SetDB(db); err != nil {
		t.Fatalf("SetDB failed: %v", err)
	}

	// 5. Verify legacy 'qqbot' key is removed and 'qqbots' contains the migrated config
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_settings WHERE key = 'qqbot'`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count legacy qqbot key: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected legacy 'qqbot' key to be deleted after migration, got count = %d", count)
	}

	currentSettings := svc.Get()
	if len(currentSettings.QQBots) != 1 {
		t.Fatalf("Expected 1 migrated QQBot config in settings, got %d", len(currentSettings.QQBots))
	}

	migrated := currentSettings.QQBots[0]
	if migrated.ID != "default" {
		t.Errorf("Expected migrated bot ID to be 'default', got %q", migrated.ID)
	}
	if migrated.AppID != "12345" || migrated.AppSecret != "secret_val" || migrated.Intents != 1024 || !migrated.Sandbox || !migrated.Enabled {
		t.Errorf("Migrated config values do not match seeded values: %+v", migrated)
	}

	// 6. Test persistence of new fields (Name, BindType, BindAgent)
	testBots := []QQBotConfig{
		{
			ID:        "bot1",
			Name:      "Support Agent",
			Enabled:   true,
			AppID:     "999",
			AppSecret: "abc",
			Intents:   1,
			Sandbox:   false,
			BindType:  "l2",
			BindAgent: "coder_agent",
		},
	}

	if err := SaveSystemSetting(ctx, db, "qqbots", testBots); err != nil {
		t.Fatalf("Failed to save updated qqbots config: %v", err)
	}

	// Reload and verify
	if err := svc.ReloadFromDB(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	loadedSettings := svc.Get()
	if len(loadedSettings.QQBots) != 1 {
		t.Fatalf("Expected 1 QQBot config after save, got %d", len(loadedSettings.QQBots))
	}

	loaded := loadedSettings.QQBots[0]
	if loaded.ID != "bot1" || loaded.Name != "Support Agent" || loaded.BindType != "l2" || loaded.BindAgent != "coder_agent" {
		t.Errorf("Loaded config does not match saved values (check JSON tags). Saved: %+v, Loaded: %+v", testBots[0], loaded)
	}
}

func TestModelVisionPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	dbPath := filepath.Join(tmpDir, "entries.db")
	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 1. Create a model with vision enabled
	m1 := LLMModel{
		ID:            "test-vision-model",
		ProviderID:    "deepseek",
		Name:          "Test Vision Model",
		APIModel:      "test-api-model",
		ContextWindow: 2048,
		Enabled:       true,
		Vision:        true,
	}

	// 2. Create another model with vision disabled
	m2 := LLMModel{
		ID:            "test-no-vision-model",
		ProviderID:    "deepseek",
		Name:          "Test No Vision Model",
		APIModel:      "test-api-model",
		ContextWindow: 2048,
		Enabled:       true,
		Vision:        false,
	}

	// First we need deepseek provider to satisfy foreign key constraint
	p := LLMProvider{
		ID:      "deepseek",
		Name:    "DeepSeek",
		Enabled: true,
	}
	if err := SaveProvider(ctx, db, p); err != nil {
		t.Fatalf("Failed to save provider: %v", err)
	}

	if err := SaveModel(ctx, db, m1); err != nil {
		t.Fatalf("Failed to save model m1: %v", err)
	}
	if err := SaveModel(ctx, db, m2); err != nil {
		t.Fatalf("Failed to save model m2: %v", err)
	}

	// 3. Load models back and verify
	models, err := LoadModels(ctx, db)
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	var foundM1, foundM2 bool
	for _, m := range models {
		if m.ID == m1.ID {
			foundM1 = true
			if !m.Vision {
				t.Errorf("Expected model %s to have Vision=true, got false", m.ID)
			}
		}
		if m.ID == m2.ID {
			foundM2 = true
			if m.Vision {
				t.Errorf("Expected model %s to have Vision=false, got true", m.ID)
			}
		}
	}

	if !foundM1 {
		t.Errorf("Model %s was not found in loaded models", m1.ID)
	}
	if !foundM2 {
		t.Errorf("Model %s was not found in loaded models", m2.ID)
	}
}
