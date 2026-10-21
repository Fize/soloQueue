package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/vectorstore"
)

func TestEngineMasterGatePreservesDataAndBoundAccess(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	l1 := e.BindL1(ScopeGlobal, "", false)
	l2, err := e.BindL2Group("12345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	candidate := MemoryCandidate{Content: "SQLite is the durable memory database", MemoryType: MemoryTypeStableFact, SourceType: SourceExplicit}
	for _, access := range []Access{l1, l2} {
		if _, err := access.Ingest(ctx, candidate); err != nil {
			t.Fatal(err)
		}
	}
	hash, _, err := e.SaveWithEntities(ctx, "SQLite links durable memories", "2026-10-21", "", "2026-10-21", []EntityExtraction{{Name: "SQLite", Relations: []RelationExtraction{{TargetName: "Memory", RelType: "stores"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AddAlias(ctx, "sqlite3", "SQLite"); err != nil {
		t.Fatal(err)
	}
	snapshot := func() [4]int {
		t.Helper()
		var counts [4]int
		for i, table := range []string{"mem_entries", "kg_nodes", "kg_edges", "kg_aliases"} {
			if err := e.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&counts[i]); err != nil {
				t.Fatal(err)
			}
		}
		return counts
	}
	before := snapshot()
	e.SetEnabled(false)
	checks := []struct {
		name string
		call func() error
	}{
		{"save", func() error { _, _, err := e.Save(ctx, "blocked", "", "", ""); return err }},
		{"entities", func() error {
			_, _, err := e.SaveWithEntities(ctx, "blocked", "", "", "", []EntityExtraction{{Name: "blocked"}})
			return err
		}},
		{"search", func() error { _, err := e.Search(ctx, SearchQuery{Text: "SQLite"}); return err }},
		{"index", func() error { _, err := e.IndexEntity(ctx, "blocked", ""); return err }},
		{"connect", func() error {
			return e.ConnectEntities(ctx, "blocked", "SQLite", "links", 1, "", "", time.Now(), nil, nil)
		}},
		{"alias", func() error { return e.AddAlias(ctx, "blocked", "SQLite") }},
		{"recall", func() error { _, err := e.RecallEntity(ctx, "SQLite", 1, 10); return err }},
		{"path", func() error { _, _, err := e.ShortestPath(ctx, "SQLite", "Memory", 2); return err }},
		{"timeline", func() error { _, err := e.Timeline(ctx, "", "", 10); return err }},
		{"scoped timeline", func() error { _, err := e.TimelineScoped(ctx, "", "", 10, ScopeGlobal, "", false); return err }},
		{"consolidate", func() error { _, err := e.Consolidate(ctx); return err }},
		{"salience", func() error { return e.BoostSalience(ctx, hash) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, ErrMemoryDisabled) {
				t.Fatal(err)
			}
		})
	}
	for _, access := range []Access{l1, l2} {
		if _, err := access.Ingest(ctx, candidate); !errors.Is(err, ErrMemoryDisabled) {
			t.Fatal(err)
		}
		if _, err := access.Search(ctx, SearchQuery{Text: "SQLite"}); !errors.Is(err, ErrMemoryDisabled) {
			t.Fatal(err)
		}
		if _, err := access.Timeline(ctx, "", "", 10); !errors.Is(err, ErrMemoryDisabled) {
			t.Fatal(err)
		}
		if _, err := access.RecallEntity(ctx, "SQLite", 1, 10); !errors.Is(err, ErrMemoryDisabled) {
			t.Fatal(err)
		}
	}
	if e.HasVector() || snapshot() != before {
		t.Fatal("disabled operations changed durable data")
	}
	e.SetEnabled(true)
	for _, access := range []Access{l1, l2} {
		found, err := access.Search(ctx, SearchQuery{Text: "SQLite"})
		if err != nil || len(found.Results) == 0 {
			t.Fatalf("same capability did not resume: %v %v", found, err)
		}
		candidate.Content += " retained"
		if _, err := access.Ingest(ctx, candidate); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEngineGateConcurrentHotReload(t *testing.T) {
	e := newTestEngine(t)
	access := e.BindL1(ScopeGlobal, "", false)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			e.SetEnabled(false)
			e.SetEnabled(true)
		}
	}()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if _, err := access.Search(context.Background(), SearchQuery{Text: "memory"}); err != nil && !errors.Is(err, ErrMemoryDisabled) {
					t.Error(err)
				}
				if _, err := access.Timeline(context.Background(), "", "", 10); err != nil && !errors.Is(err, ErrMemoryDisabled) {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	e.SetEnabled(false)
	if _, err := access.Search(context.Background(), SearchQuery{Text: "memory"}); !errors.Is(err, ErrMemoryDisabled) {
		t.Fatal(err)
	}
}

// Observe the real store/SQL admission boundary without production test hooks.
type gateObservedContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *gateObservedContext) Err() error {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Err()
}
func (c *gateObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

type gateTestEmbedder struct {
	calls atomic.Int32
	block func([]string)
}

func (e *gateTestEmbedder) Dimension() int { return 3 }
func (e *gateTestEmbedder) Embed(_ context.Context, texts []string) ([]embedding.Result, error) {
	e.calls.Add(1)
	if e.block != nil {
		e.block(texts)
	}
	return []embedding.Result{{Embedding: []float32{1, 0, 0}, Tokens: 1}}, nil
}

func assertMemoryOffDrains(t *testing.T, entered <-chan struct{}, release func(), operation <-chan error, disable func()) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not reach pause point")
	}
	off := make(chan struct{})
	go func() { disable(); close(off) }()
	select {
	case <-off:
		t.Error("OFF returned before the admitted operation drained")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-operation:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admitted operation deadlocked")
	}
	select {
	case <-off:
	case <-time.After(3 * time.Second):
		t.Fatal("OFF did not finish after operation drained")
	}
}

func TestEngineOffDrainsAdmittedWrites(t *testing.T) {
	for _, kind := range []string{"save", "save_entities", "l1_ingest", "l2_ingest"} {
		t.Run(kind, func(t *testing.T) {
			db, mu := openTestDB(t)
			emb := &gateTestEmbedder{}
			vectors := vectorstore.NewSQLiteStoreFromDB(db, mu)
			e := New(db, mu, emb, vectors, nil)
			l1 := e.BindL1(ScopeGlobal, "", false)
			l2, err := e.BindL2Group("12345678-1234-1234-1234-123456789abc")
			if err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{})
			ctx := &gateObservedContext{Context: context.Background(), entered: entered}
			result := make(chan error, 1)
			mu.Lock()
			var once sync.Once
			release := func() { once.Do(mu.Unlock) }
			t.Cleanup(release)
			go func() {
				entities := []EntityExtraction{{Name: "durable entity"}}
				var err error
				switch kind {
				case "save":
					_, _, err = e.Save(ctx, "durable memory", "2026-10-21", "", "2026-10-21")
				case "save_entities":
					_, _, err = e.SaveWithEntities(ctx, "durable memory", "2026-10-21", "", "2026-10-21", entities)
				default:
					access := l1
					if kind == "l2_ingest" {
						access = l2
					}
					_, err = access.Ingest(ctx, MemoryCandidate{Content: "durable memory", MemoryType: MemoryTypeStableFact, SourceType: SourceExplicit, Entities: entities})
				}
				result <- err
			}()
			assertMemoryOffDrains(t, entered, release, result, func() { e.SetEnabled(false) })
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM mem_entries").Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
			calls := emb.calls.Load()
			if calls != 1 {
				t.Fatalf("admitted embedding did not finish before OFF: %d", calls)
			}
			if _, _, err := e.Save(context.Background(), "late memory", "", "", ""); !errors.Is(err, ErrMemoryDisabled) {
				t.Fatal(err)
			}
			if emb.calls.Load() != calls {
				t.Fatal("embedding started after OFF")
			}
			e.SetEnabled(true)
			found, err := l1.Search(context.Background(), SearchQuery{Text: "durable"})
			if kind == "l2_ingest" {
				found, err = l2.Search(context.Background(), SearchQuery{Text: "durable"})
			}
			if err != nil || len(found.Results) == 0 {
				t.Fatal("re-enable lost admitted memory", found, err)
			}
		})
	}
}

func TestEngineOffDrainsAdmittedSearchAndBoundTimeline(t *testing.T) {
	for _, owner := range []string{"l1", "l2"} {
		for _, kind := range []string{"search", "timeline", "recall"} {
			t.Run(owner+"_"+kind, func(t *testing.T) {
				db, mu := openTestDB(t)
				emb := &gateTestEmbedder{}
				e := New(db, mu, emb, vectorstore.NewSQLiteStoreFromDB(db, mu), nil)
				access := e.BindL1(ScopeGlobal, "", false)
				if owner == "l2" {
					var err error
					access, err = e.BindL2Group("12345678-1234-1234-1234-123456789abc")
					if err != nil {
						t.Fatal(err)
					}
				}
				_, err := access.Ingest(context.Background(), MemoryCandidate{Content: "SQLite durable memory", MemoryType: MemoryTypeStableFact, SourceType: SourceExplicit})
				if err != nil {
					t.Fatal(err)
				}
				entered := make(chan struct{})
				result := make(chan error, 1)
				var release func()
				ctx := context.Context(context.Background())
				if kind == "timeline" {
					// Exhaust the real SQL pool to suspend the already-admitted bound read.
					db.SetMaxOpenConns(1)
					conn, err := db.Conn(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					var once sync.Once
					release = func() { once.Do(func() { _ = conn.Close() }) }
					ctx = &gateObservedContext{Context: context.Background(), entered: entered}
				} else {
					gate := make(chan struct{})
					var once sync.Once
					release = func() { once.Do(func() { close(gate) }) }
					emb.block = func(texts []string) {
						if strings.HasPrefix(texts[0], "query: ") {
							close(entered)
							<-gate
						}
					}
				}
				t.Cleanup(release)
				go func() {
					var err error
					if kind == "timeline" {
						var memories []MemoryEntry
						memories, err = access.Timeline(ctx, "", "", 10)
						if err == nil && len(memories) != 1 {
							err = errors.New("timeline lost retained memory")
						}
					} else if kind == "recall" {
						_, err = access.RecallEntity(ctx, "SQLite", 2, 10)
					} else {
						var found *SearchResultSet
						found, err = access.Search(ctx, SearchQuery{Text: "SQLite"})
						if err == nil && len(found.Results) == 0 {
							err = errors.New("search lost retained memory")
						}
					}
					result <- err
				}()
				assertMemoryOffDrains(t, entered, release, result, func() { e.SetEnabled(false) })
				calls := emb.calls.Load()
				if _, err := access.Search(context.Background(), SearchQuery{Text: "SQLite"}); !errors.Is(err, ErrMemoryDisabled) {
					t.Fatal(err)
				}
				if _, err := access.Timeline(context.Background(), "", "", 10); !errors.Is(err, ErrMemoryDisabled) {
					t.Fatal(err)
				}
				if emb.calls.Load() != calls {
					t.Fatal("embedding started after OFF")
				}
				emb.block = nil
				e.SetEnabled(true)
				if _, err := access.Search(context.Background(), SearchQuery{Text: "SQLite"}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
