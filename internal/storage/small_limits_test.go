package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger/rotating"
	"github.com/xiaobaitu/soloqueue/internal/observability"
)

func treeStats(t *testing.T, root string) (int, int64) {
	t.Helper()
	var count int
	var size int64
	err := filepath.WalkDir(root, func(path string, en os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !en.IsDir() {
			st, e := en.Info()
			if e != nil {
				return e
			}
			if st.Mode().IsRegular() {
				count++
				size += st.Size()
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count, size
}
func ageRequest(t *testing.T, store *observability.Store, request string) {
	t.Helper()
	units, err := store.RetentionUnits()
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range units {
		p := filepath.Join(store.Root(), "requests", unit.ID, "meta.json")
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var meta map[string]any
		if err = json.Unmarshal(data, &meta); err != nil {
			t.Fatal(err)
		}
		if meta["request_id"] != request {
			continue
		}
		meta["finished_at"] = time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
		data, _ = json.Marshal(meta)
		if err = os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("request not found", request)
}
func TestSmallInstalledLimitsEndToEnd(t *testing.T) {
	root := t.TempDir()
	policy := config.DefaultStorageConfig()
	policy.RuntimeLogs.FilePolicy = config.FilePolicy{MaxFileMB: 1, MaxTotalMB: 1, MaxFiles: 1, RetentionDays: 1}
	policy.ExecutionDetails.FilePolicy = config.FilePolicy{MaxFileMB: 1, MaxTotalMB: 1, MaxFiles: 1, RetentionDays: 1}
	policy.ExecutionDetails.MaxTurnMB = 1
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	manager := New(root, func() config.StorageConfig { return policy }, nil)
	defer manager.Close()
	store := observability.ForWorkDir(root)
	prompt := func(text string) *observability.Prompt {
		return &observability.Prompt{RecordingStatus: "complete", Messages: []observability.Instruction{{Role: "system", Content: text}}}
	}
	// Each body separately fits 1MiB; their combined final state must still obey 1MiB.
	if err := store.Start("l1", "large"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCall(observability.Call{CallID: "large", SessionID: "l1", RequestID: "large", Content: strings.Repeat("x", 600<<10), StartedAt: time.Now()}, prompt(strings.Repeat("p", 600<<10))); err != nil {
		t.Fatal(err)
	}
	large, err := store.Request("l1", "large")
	if err != nil || len(large.Calls) != 1 || large.Calls[0].RecordingStatus != "partial" {
		t.Fatalf("aggregate limit failed: %+v %v", large, err)
	}
	if _, size := treeStats(t, store.Root()); size > 1<<20 {
		t.Fatalf("quota exceeded %d", size)
	}
	// Use a fresh work directory for an independent retention/GC scenario under the same policy.
	root2 := t.TempDir()
	manager2 := New(root2, func() config.StorageConfig { return policy }, nil)
	defer manager2.Close()
	store = observability.ForWorkDir(root2)
	shared := prompt(strings.Repeat("shared", 20000))
	for _, req := range []string{"old-shared", "old-unique", "retained", "active"} {
		if err := store.Start("l1", req); err != nil {
			t.Fatal(err)
		}
		p := shared
		if req == "old-unique" {
			p = prompt(strings.Repeat("unique", 20000))
		}
		count := 1
		if req == "old-shared" {
			count = 2
		}
		for i := 0; i < count; i++ {
			id := string(rune('a' + i))
			if err := store.SaveCall(observability.Call{CallID: id, SessionID: "l1", RequestID: req, StartedAt: time.Now(), Content: strings.Repeat("body", 10000)}, p); err != nil {
				t.Fatal(err)
			}
		}
		if req != "active" {
			if err := store.Finish("l1", req, "completed"); err != nil {
				t.Fatal(err)
			}
		}
		if strings.HasPrefix(req, "old-") {
			ageRequest(t, store, req)
		}
	}
	prompts, _ := filepath.Glob(filepath.Join(store.Root(), "prompts", "*.json"))
	if len(prompts) != 2 {
		t.Fatalf("dedup failed %d", len(prompts))
	}
	beforeFiles, beforeBytes := treeStats(t, store.Root())
	preview, err := manager2.Preview(policy, Revision(policy), false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ProtectedFiles < 1 {
		t.Fatal("active not protected")
	}
	result, err := manager2.Execute(preview.ID, preview.Revision, true)
	if err != nil || len(result.Errors) > 0 {
		t.Fatalf("cleanup %+v %v", result, err)
	}
	afterFiles, afterBytes := treeStats(t, store.Root())
	if result.DeletedFiles != beforeFiles-afterFiles || result.DeletedFiles != 4 || result.FreedBytes != beforeBytes-afterBytes {
		t.Fatalf("actual cleanup stats mismatch %+v actual files=%d bytes=%d", result, beforeFiles-afterFiles, beforeBytes-afterBytes)
	}
	for _, req := range []string{"retained", "active"} {
		r, e := store.Request("l1", req)
		if e != nil || len(r.Calls) != 1 || !r.Calls[0].PromptAvailable {
			t.Fatalf("protected/shared data lost %+v %v", r, e)
		}
	}
	// Real 1MiB writer policy rotates and keeps one archived file plus the active file.
	dir := filepath.Join(root2, "logs", "system")
	writer, err := rotating.OpenDateSize(dir, "tool", 50<<20, 15)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for i := 0; i < 4; i++ {
		if _, err = writer.Write([]byte(strings.Repeat("l", 700<<10))); err != nil {
			t.Fatal(err)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "tool-*.jsonl"))
	if len(paths) != 2 {
		t.Fatalf("maxFiles=1 archive count mismatch %v", paths)
	}
	active := ""
	for _, path := range paths {
		st, _ := os.Stat(path)
		if st.Size() > 1<<20 {
			t.Fatalf("rotation size %d", st.Size())
		}
		if rotating.IsActive(path) {
			active = path
		}
	}
	if active == "" {
		t.Fatal("no active writer")
	}
	old := filepath.Join(dir, "tool-"+time.Now().AddDate(0, 0, -2).Format("2006-01-02")+".jsonl")
	if err = os.WriteFile(old, []byte("expired"), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err = manager2.Preview(policy, Revision(policy), false)
	if err != nil {
		t.Fatal(err)
	}
	result, err = manager2.Execute(preview.ID, preview.Revision, true)
	if err != nil || len(result.Errors) > 0 {
		t.Fatalf("small log cleanup %+v %v", result, err)
	}
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("one-day retention not applied", err)
	}
	if _, err = os.Stat(active); err != nil {
		t.Fatal("active file removed", err)
	}
}
