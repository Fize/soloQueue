package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/storagepolicy"
)

// A delegation journal written under a cron execution belongs to cron_history.
// The cleaner must not charge it to execution_details, or the two categories
// drift apart again.
func TestCronDelegationJournalCountsAsCronHistory(t *testing.T) {
	root := t.TempDir()
	rel := "logs/cron/task/delegations/dlg_1/stream-2020-01-01.jsonl"
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("{\"sequence\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(full, old, old); err != nil {
		t.Fatal(err)
	}
	policy := config.DefaultStorageConfig()
	manager := New(root, func() config.StorageConfig { return policy }, nil)
	defer manager.Close()

	usage := manager.Usage()
	for _, category := range usage.Categories {
		if category.ID == "execution_details" && category.Bytes != 0 {
			t.Fatalf("cron journal charged to execution_details: %+v", category)
		}
		if category.ID == "cron_history" && category.Bytes == 0 {
			t.Fatal("cron journal missing from cron_history")
		}
	}
	ledger := storagepolicy.Scan(root)
	for _, unit := range ledger.Units {
		if unit.Key == "cron/task" && unit.Category != storagepolicy.CronHistory {
			t.Fatalf("cron unit category = %s", unit.Category)
		}
	}
}
