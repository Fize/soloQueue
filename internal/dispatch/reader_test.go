package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func journalFixture(t testing.TB, n, payload int) (string, Record) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "delegations", "dlg_page")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	rec := Record{ID: "dlg_page", OwnerSessionID: "l1-session", RequestID: "req", Status: StatusCompleted, Revision: uint64(n)}
	f, err := os.Create(filepath.Join(dir, "stream-2026-01-01.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for i := 1; i <= n; i++ {
		if err := enc.Encode(Event{Sequence: uint64(i), Record: rec, Type: "progress", Payload: json.RawMessage(fmt.Sprintf("%q", strings.Repeat("x", payload)))}); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return dir, rec
}
func TestPagedLegacyJournalExceedsWholeReadBudget(t *testing.T) {
	dir, rec := journalFixture(t, 270, 300<<10)
	if _, err := ReadEvents(dir); !errors.Is(err, ErrReadLimit) {
		t.Fatalf("unbounded whole read accepted: %v", err)
	}
	cursor := ""
	total := 0
	for {
		page, err := ReadEventPageForRecord(dir, rec, cursor, 13)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range page.Events {
			total++
			if ev.Sequence != uint64(total) {
				t.Fatalf("duplicate/skipped %d != %d", ev.Sequence, total)
			}
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = page.NextCursor
	}
	if total != 270 {
		t.Fatal(total)
	}
	page, err := ReadEventPageForRecord(dir, rec, "2", 1)
	if err != nil || len(page.Events) != 1 || page.Events[0].Sequence != 3 {
		t.Fatalf("legacy cursor %v %v", page, err)
	}
	manager, err := NewManager(filepath.Dir(filepath.Dir(dir)), rec.OwnerSessionID)
	if err != nil {
		t.Fatalf("large legacy recovery failed: %v", err)
	}
	recovered, ok := manager.Get(rec.ID)
	if !ok || recovered.Revision != rec.Revision || recovered.Status != StatusCompleted {
		t.Fatalf("recovery lost latest record: %+v", recovered)
	}
	stats, err := CompactCompletedDetailsWithStats(dir)
	if err != nil || stats.FreedBytes < 64<<20 {
		t.Fatalf("large legacy compaction failed: %+v %v", stats, err)
	}
}
func TestDispatchReadLimitsBeforeDecode(t *testing.T) {
	for _, file := range []string{"stream-2026-01-01.jsonl", "state.json"} {
		t.Run(file, func(t *testing.T) {
			dir, rec := journalFixture(t, 0, 0)
			f, err := os.Create(filepath.Join(dir, file))
			if err != nil {
				t.Fatal(err)
			}
			// A sparse gigabyte file must never become a gigabyte allocation.
			if err = f.Truncate(1 << 30); err != nil {
				t.Fatal(err)
			}
			f.Close()
			_, err = ReadEventPageForRecord(dir, rec, "", 1)
			if !errors.Is(err, ErrReadLimit) {
				t.Fatalf("size limit not explicit: %v", err)
			}
		})
	}
}
func TestDispatchPageIdentityAndCheckpoint(t *testing.T) {
	dir, rec := journalFixture(t, 3, 1)
	checkpoint := Event{Sequence: 4, Record: rec, Type: "terminal"}
	b, _ := json.MarshalIndent(checkpoint, "", "  ")
	os.WriteFile(filepath.Join(dir, "state.json"), b, 0600)
	first, err := ReadEventPageForRecord(dir, rec, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReadEventPageForRecord(dir, rec, first.NextCursor, 2)
	if err != nil || len(second.Events) != 2 || second.Events[1].Sequence != 4 {
		t.Fatalf("checkpoint %v %v", second, err)
	}
	rec.RequestID = "other"
	if _, err := ReadEventPageForRecord(dir, rec, first.NextCursor, 2); err == nil {
		t.Fatal("identity crossed cursor")
	}
}
func BenchmarkLegacyDispatchRead(b *testing.B) {
	dir, rec := journalFixture(b, 2048, 4096)
	b.Run("bounded_page", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := ReadEventPageForRecord(dir, rec, "", 10); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("previous_full_decode", func(b *testing.B) {
		data, err := os.ReadFile(filepath.Join(dir, "stream-2026-01-01.jsonl"))
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			decoder := json.NewDecoder(bytes.NewReader(data))
			var events []Event
			for decoder.More() {
				var ev Event
				if err := decoder.Decode(&ev); err != nil {
					b.Fatal(err)
				}
				events = append(events, ev)
			}
			if len(events) != 2048 {
				b.Fatal(len(events))
			}
		}
	})
}

func TestCheckpointContinuesAfterPageByteBudget(t *testing.T) {
	dir, rec := journalFixture(t, 10, 3<<20)
	checkpoint := Event{Sequence: 11, Record: rec, Type: "terminal", Payload: json.RawMessage(fmt.Sprintf("%q", strings.Repeat("c", 3<<20)))}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cursor := ""
	total := 0
	for pages := 0; ; pages++ {
		if pages > 2 {
			t.Fatal("pagination did not terminate")
		}
		page, err := ReadEventPageForRecord(dir, rec, cursor, 100)
		if err != nil {
			t.Fatal(err)
		}
		if pages == 0 && (len(page.Events) != 10 || page.NextCursor == "") {
			t.Fatalf("expected journal events and checkpoint continuation, got %d events, cursor %q", len(page.Events), page.NextCursor)
		}
		for _, event := range page.Events {
			total++
			if event.Sequence != uint64(total) {
				t.Fatalf("duplicate/skipped sequence: got %d want %d", event.Sequence, total)
			}
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = page.NextCursor
	}
	if total != 11 {
		t.Fatalf("got %d events, want 11", total)
	}
}

func TestRecoveryCumulativeReadBudget(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		t.Run(fmt.Sprintf("checkpoint=%t", checkpoint), func(t *testing.T) {
			dir, rec := journalFixture(t, 0, 0)
			whitespace := bytes.Repeat([]byte(" "), maxDispatchRecordBytes)
			files := maxDispatchHistoryBytes / maxDispatchRecordBytes
			if !checkpoint {
				files++
			}
			for i := 0; i < files; i++ {
				name := filepath.Join(dir, fmt.Sprintf("stream-2026-02-%02d.jsonl", i+1))
				if err := os.WriteFile(name, whitespace, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if checkpoint {
				data, err := json.Marshal(Event{Sequence: 1, Record: rec, Type: "terminal"})
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readLatestEvent(dir); !errors.Is(err, ErrReadLimit) {
				t.Fatalf("cumulative recovery limit not enforced: %v", err)
			}
		})
	}
}
