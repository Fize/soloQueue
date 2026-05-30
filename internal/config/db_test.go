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
