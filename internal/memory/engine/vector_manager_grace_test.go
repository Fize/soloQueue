package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/local"
)

// A disable inside the grace window must not reload the 1.2GB local model.
func TestLocalEmbeddingGraceReusesWorker(t *testing.T) {
	previous := stopGrace
	stopGrace = 300 * time.Millisecond
	defer func() { stopGrace = previous }()

	db, mu := openTestDB(t)
	var starts atomic.Int32
	workers := make([]*managedTestWorker, 0, 2)
	options := testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
		starts.Add(1)
		worker := &managedTestWorker{}
		workers = append(workers, worker)
		return worker, nil
	})
	manager := NewVectorManager(db, mu, t.TempDir(), options)
	defer manager.Close()

	enabled := localVectorConfig()
	manager.Apply(enabled, nil, nil)
	waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })

	disabled := enabled
	disabled.Enabled = false
	manager.Apply(disabled, nil, nil)
	if state := manager.Status().State; state != "disabled" {
		t.Fatalf("config state must flip immediately, got %q", state)
	}
	if manager.Enabled() {
		t.Fatal("suspended manager must report disabled")
	}
	if len(workers) != 1 || workers[0].closed.Load() {
		t.Fatal("worker closed inside the grace window")
	}

	manager.Apply(enabled, nil, nil)
	waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })
	if got := starts.Load(); got != 1 {
		t.Fatalf("worker restarted inside the grace window: %d starts", got)
	}
	if !manager.Enabled() {
		t.Fatal("resumed manager must report enabled")
	}
}

func TestLocalEmbeddingClosesAfterGrace(t *testing.T) {
	previous := stopGrace
	stopGrace = 40 * time.Millisecond
	defer func() { stopGrace = previous }()

	db, mu := openTestDB(t)
	var starts atomic.Int32
	workers := make([]*managedTestWorker, 0, 1)
	options := testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
		starts.Add(1)
		worker := &managedTestWorker{}
		workers = append(workers, worker)
		return worker, nil
	})
	manager := NewVectorManager(db, mu, t.TempDir(), options)
	defer manager.Close()

	enabled := localVectorConfig()
	manager.Apply(enabled, nil, nil)
	waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })

	disabled := enabled
	disabled.Enabled = false
	manager.Apply(disabled, nil, nil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if workers[0].closed.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("worker still running after the grace window")
}

// A model or device change is a real restart, not a suspend.
func TestLocalEmbeddingModelChangeRestartsImmediately(t *testing.T) {
	previous := stopGrace
	stopGrace = 10 * time.Second
	defer func() { stopGrace = previous }()

	db, mu := openTestDB(t)
	var starts atomic.Int32
	workers := make([]*managedTestWorker, 0, 2)
	options := testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
		starts.Add(1)
		worker := &managedTestWorker{}
		workers = append(workers, worker)
		return worker, nil
	})
	manager := NewVectorManager(db, mu, t.TempDir(), options)
	defer manager.Close()

	enabled := localVectorConfig()
	manager.Apply(enabled, nil, nil)
	waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })

	moved := enabled
	moved.Device = "cpu"
	manager.Apply(moved, nil, nil)
	waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })
	if got := starts.Load(); got != 2 {
		t.Fatalf("device change must restart the worker, starts = %d", got)
	}
}

func TestLocalEmbeddingSuspendedTransitions(t *testing.T) {
	for _, transition := range []string{"repeat-disabled", "backend", "model", "device", "retry", "reindex", "cancel", "close"} {
		t.Run(transition, func(t *testing.T) {
			db, mu := openTestDB(t)
			var starts atomic.Int32
			manager := NewVectorManager(db, mu, t.TempDir(), testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
				starts.Add(1)
				return &managedTestWorker{}, nil
			}))
			defer manager.Close()
			enabled := localVectorConfig()
			manager.Apply(enabled, nil, nil)
			waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })
			manager.mu.Lock()
			old := manager.binding
			manager.mu.Unlock()
			disabled := enabled
			disabled.Enabled = false
			manager.Apply(disabled, nil, nil)
			manager.mu.Lock()
			originalTimer := manager.stopTimer
			manager.mu.Unlock()
			if transition == "repeat-disabled" {
				disabled.Provider = "none"
				manager.Apply(disabled, nil, nil)
				manager.Apply(disabled, nil, nil)
				manager.mu.Lock()
				sameTimer := manager.stopTimer == originalTimer
				manager.mu.Unlock()
				if !sameTimer {
					t.Fatal("repeated disable reset grace deadline")
				}
			} else if transition == "backend" {
				changed := disabled
				changed.Provider = "openai"
				manager.Apply(changed, nil, nil)
			} else if transition == "model" || transition == "device" {
				changed := disabled
				if transition == "model" {
					changed.Model = "other"
				} else {
					changed.Device = "cpu"
				}
				manager.Apply(changed, nil, nil)
			} else if transition == "close" {
				manager.Close()
				manager.mu.Lock()
				parked, timer := manager.suspended, manager.stopTimer
				manager.mu.Unlock()
				if parked || timer != nil || old.ctx.Err() == nil {
					t.Fatal("Close retained parked lifecycle")
				}
				return
			} else {
				manager.Apply(enabled, nil, nil)
				if _, err := manager.Action(transition); err != nil {
					t.Fatal(err)
				}
				if transition == "cancel" {
					manager.Apply(disabled, nil, nil)
					manager.Apply(enabled, nil, nil)
				}
			}
			manager.Apply(enabled, nil, nil)
			waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })
			manager.mu.Lock()
			binding, parked, timer := manager.binding, manager.suspended, manager.stopTimer
			manager.mu.Unlock()
			if binding == nil || binding.ctx.Err() != nil || parked || timer != nil {
				t.Fatal("retained stale lifecycle")
			}
			want := int32(2)
			if transition == "repeat-disabled" {
				want = 1
			}
			if starts.Load() != want {
				t.Fatalf("starts=%d want %d", starts.Load(), want)
			}
		})
	}
}

// Failures released after disable must invalidate the parked worker before the grace deadline.
func TestLocalEmbeddingSuspendedFailures(t *testing.T) {
	for _, failure := range []string{"query", "catchup-inference", "catchup-storage"} {
		t.Run(failure, func(t *testing.T) {
			db, mu := openTestDB(t)
			entered, release, searched := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var starts atomic.Int32
			var freshQueries atomic.Int32
			var first *managedTestWorker
			manager := NewVectorManager(db, mu, t.TempDir(), testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
				worker := &managedTestWorker{}
				if starts.Add(1) == 1 {
					first = worker
					worker.embed = func(ctx context.Context, texts []string, purpose string) (local.Batch, error) {
						if (failure == "query" && purpose == "query") || (failure != "query" && purpose == "document") {
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
								return local.Batch{}, ctx.Err()
							}
							if failure != "catchup-storage" {
								return local.Batch{}, errors.New("injected worker failure")
							}
						}
						return testLocalBatch(texts), nil
					}
				} else {
					worker.embed = func(_ context.Context, texts []string, purpose string) (local.Batch, error) {
						if purpose == "query" {
							freshQueries.Add(1)
						}
						return testLocalBatch(texts), nil
					}
				}
				return worker, nil
			}))
			defer manager.Close()
			defer unblock()
			enabled := localVectorConfig()
			manager.Apply(enabled, nil, nil)
			waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })
			disabled := enabled
			disabled.Enabled = false
			if failure != "query" {
				manager.Apply(disabled, nil, nil)
			}
			if failure == "query" {
				go func() {
					defer close(searched)
					_, _ = manager.Search(context.Background(), SearchQuery{Text: "query", OwnerType: OwnerL1}, 5)
				}()
			} else {
				if _, err := db.Exec(`INSERT INTO mem_entries(id,content,content_hash,date,event_time,scope_type,scope_id) VALUES('grace-failure','grace failure','grace-failure-hash','2026-01-01','2026-01-01','global','')`); err != nil {
					t.Fatal(err)
				}
				manager.Notify()
			}
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("inference did not start")
			}
			manager.Apply(disabled, nil, nil)
			if failure == "catchup-storage" {
				if _, err := db.Exec(`CREATE TRIGGER grace_storage_failure BEFORE INSERT ON mem_vec_local BEGIN SELECT RAISE(FAIL,'injected storage failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			unblock()
			if failure == "query" {
				select {
				case <-searched:
				case <-time.After(3 * time.Second):
					t.Fatal("query did not finish")
				}
			}
			deadline := time.Now().Add(3 * time.Second)
			for !first.closed.Load() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if !first.closed.Load() {
				t.Fatal("failed parked worker was not closed")
			}
			if status := manager.Status(); status.State != "disabled" || manager.Enabled() {
				t.Fatalf("failure changed disabled presentation: %+v", status)
			}
			_, err := manager.Search(context.Background(), SearchQuery{Text: "query", OwnerType: OwnerL1}, 5)
			if failure == "catchup-storage" {
				if err == nil || !strings.Contains(err.Error(), "injected storage failure") {
					t.Fatalf("storage failure lost: %v", err)
				}
				if _, err := db.Exec(`DROP TRIGGER grace_storage_failure`); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			manager.Apply(enabled, nil, nil)
			waitVectorStatus(t, manager, func(s EmbeddingStatus) bool { return s.State == "ready" })
			if starts.Load() != 2 {
				t.Fatalf("failed worker resumed: starts=%d", starts.Load())
			}
			results, err := manager.Search(context.Background(), SearchQuery{Text: "query", OwnerType: OwnerL1}, 5)
			if err != nil {
				t.Fatalf("fresh worker search failed: %v", err)
			}
			if freshQueries.Load() != 1 {
				t.Fatal("search did not use the fresh worker")
			}
			if failure != "query" && len(results) != 1 {
				t.Fatalf("fresh worker did not finish catchup: %v", results)
			}
		})
	}
}
