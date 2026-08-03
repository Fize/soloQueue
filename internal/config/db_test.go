package config

import (
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

func TestSetDB_DoesNotSeed(t *testing.T) {
	dir := t.TempDir()
	db, err := db.Open(filepath.Join(dir, "soloqueue.db"))
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
