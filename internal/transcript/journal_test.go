package transcript

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

func TestPublishAssignsContiguousSequencesAndReplays(t *testing.T) {
	j := newJournal("l1", 41, Config{})
	for i := 0; i < 5; i++ {
		f := j.Publish(Frame{Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:0", Text: "x"})
		if f.Seq != uint64(41+i) || f.TS == "" {
			t.Fatalf("frame %d: seq=%d ts=%q", i, f.Seq, f.TS)
		}
	}
	frames, err := j.ReadAfter(j.Epoch(), 42, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 || frames[0].Seq != 43 || frames[2].Seq != 45 {
		t.Fatalf("replay = %#v", frames)
	}
	if frames, err := j.ReadAfter(j.Epoch(), 45, 0); err != nil || len(frames) != 0 {
		t.Fatalf("head read = %v %v", frames, err)
	}
	if _, err := j.ReadAfter("other", 42, 0); !errors.Is(err, ErrEpoch) {
		t.Fatalf("epoch mismatch err = %v", err)
	}
	if _, err := j.ReadAfter(j.Epoch(), 99, 0); !errors.Is(err, ErrLagged) {
		t.Fatalf("cursor ahead of head err = %v", err)
	}
	if frames, _ := j.ReadAfter(j.Epoch(), 40, 2); len(frames) != 2 || frames[0].Seq != 41 {
		t.Fatalf("bounded read = %#v", frames)
	}
}

func TestRingOverflowRequiresResync(t *testing.T) {
	j := newJournal("l1", 1, Config{MaxFrames: 3})
	for i := 0; i < 5; i++ {
		j.Publish(Frame{Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:0", Text: "x"})
	}
	if _, err := j.ReadAfter(j.Epoch(), 1, 0); !errors.Is(err, ErrLagged) {
		t.Fatalf("expired cursor err = %v", err)
	}
	frames, err := j.ReadAfter(j.Epoch(), 2, 0)
	if err != nil || len(frames) != 3 || frames[0].Seq != 3 {
		t.Fatalf("oldest retained read = %#v %v", frames, err)
	}
}

func TestConcurrentPublishersShareOneOrder(t *testing.T) {
	j := newJournal("l1", 1, Config{})
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				j.Publish(Frame{Kind: KindSessionMeta})
			}
		}()
	}
	wg.Wait()
	frames, err := j.ReadAfter(j.Epoch(), 0, 0)
	if err != nil || len(frames) != 1600 {
		t.Fatalf("frames=%d err=%v", len(frames), err)
	}
	for i, f := range frames {
		if f.Seq != uint64(i+1) {
			t.Fatalf("frame %d has seq %d", i, f.Seq)
		}
	}
}

func TestWaitWakesOnPublish(t *testing.T) {
	j := newJournal("l1", 1, Config{})
	ch := j.Wait()
	go j.Publish(Frame{Kind: KindSessionMeta})
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("publish did not wake waiters")
	}
}

func TestViewMaterializesItems(t *testing.T) {
	v := NewView()
	apply := func(f Frame) { v.Apply(f) }
	apply(Frame{Seq: 1, Kind: KindInputQueued, RequestID: "q", Text: "later"})
	apply(Frame{Seq: 2, Kind: KindInputCommitted, RequestID: "r", ItemID: "in:r", Text: "hello", ReceivedAt: "t0"})
	apply(Frame{Seq: 3, Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:0", Channel: ChannelReasoning, Text: "think "})
	apply(Frame{Seq: 4, Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:0", Channel: ChannelReasoning, Text: "more"})
	apply(Frame{Seq: 5, Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:0", Channel: ChannelContent, Text: "answer"})
	apply(Frame{Seq: 6, Kind: KindToolStarted, RequestID: "r", ItemID: "out:r:0", CallID: "c1", Name: "echo", Args: "{}"})
	apply(Frame{Seq: 7, Kind: KindInputCommitted, RequestID: "q", ItemID: "in:q", Text: "later"})
	apply(Frame{Seq: 8, Kind: KindToolFinished, RequestID: "r", CallID: "c1", Result: "ok"})
	apply(Frame{Seq: 9, Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:1", Channel: ChannelContent, Text: "resumed"})
	apply(Frame{Seq: 10, Kind: KindRequestEnd, RequestID: "r", RequestIDs: []string{"r", "q"}, TerminalCode: "error", Error: "boom"})

	items := v.List()
	var ids []string
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	want := []string{"in:r", "out:r:0", "in:q", "out:r:1", "err:r"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	first := items[1]
	if len(first.Segments) != 3 || first.Segments[0].Text != "think more" || first.Segments[1].Text != "answer" {
		t.Fatalf("segments = %#v", first.Segments)
	}
	tool := first.Segments[2]
	if tool.CallID != "c1" || tool.Result != "ok" || tool.Done == nil || !*tool.Done {
		t.Fatalf("tool = %#v", tool)
	}
	if !first.Done || len(v.Pending) != 0 {
		t.Fatalf("done=%v pending=%v", first.Done, v.Pending)
	}
	if r := v.Requests["r"]; r == nil || !r.Ended || r.Error != "boom" {
		t.Fatalf("request = %#v", r)
	}
}

func TestViewSplitsAggregatedInput(t *testing.T) {
	v := NewView()
	v.Apply(Frame{Seq: 3, Kind: KindInputCommitted, RequestID: "b", ItemID: "in:b", RequestIDs: []string{"a", "b"},
		Parts: []InputPart{{RequestID: "a", ItemID: "in:a", Text: "one"}, {RequestID: "b", ItemID: "in:b", Text: "two"}}})
	items := v.List()
	if len(items) != 2 || items[0].ID != "in:a" || items[1].ID != "in:b" || items[1].Sub != 1 {
		t.Fatalf("items = %#v", items)
	}
}

func TestEndedRequestsAreEvictedAfterGrace(t *testing.T) {
	now := time.Unix(1000, 0)
	j := newJournal("l1", 1, Config{Grace: time.Minute, Now: func() time.Time { return now }})
	j.Publish(Frame{Kind: KindInputCommitted, RequestID: "r", ItemID: "in:r", Text: "hi"})
	j.Publish(Frame{Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:0", Text: "yo"})
	j.Publish(Frame{Kind: KindRequestEnd, RequestID: "r", TerminalCode: "completed"})
	if len(j.Snapshot().Items) != 2 {
		t.Fatal("items evicted before grace")
	}
	now = now.Add(2 * time.Minute)
	j.Publish(Frame{Kind: KindSessionMeta})
	snap := j.Snapshot()
	if len(snap.Items) != 0 || len(snap.Requests) != 0 {
		t.Fatalf("items not evicted: %#v", snap)
	}
	if _, _, ok := j.ItemOpened("out:r:0"); ok {
		t.Fatal("opened index not evicted")
	}
}

func TestResetKeepsOnlyActiveRequests(t *testing.T) {
	j := newJournal("l1", 1, Config{})
	j.Publish(Frame{Kind: KindRequestStarted, RequestID: "old"})
	j.Publish(Frame{Kind: KindItemDelta, RequestID: "old", ItemID: "out:old:0", Text: "a"})
	j.Publish(Frame{Kind: KindRequestEnd, RequestID: "old"})
	j.Publish(Frame{Kind: KindRequestStarted, RequestID: "live"})
	j.Publish(Frame{Kind: KindItemDelta, RequestID: "live", ItemID: "out:live:0", Text: "b"})
	j.Publish(Frame{Kind: KindTranscriptReset, Reason: "clear"})
	items := j.Snapshot().Items
	if len(items) != 1 || items[0].ID != "out:live:0" {
		t.Fatalf("items = %#v", items)
	}
}

func TestRegistryContinuesPersistedSequence(t *testing.T) {
	dir := t.TempDir()
	tl, err := timeline.NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = tl.AppendMessage(&timeline.MessagePayload{Role: "user", Content: "a", ItemID: "in:r", Seq: 17})
	_ = tl.AppendControl(&timeline.ControlPayload{Action: "request_error", Seq: 23})
	_ = tl.Close()
	r := NewRegistry(Config{})
	j := r.Journal("l2:x", dir)
	if f := j.Publish(Frame{Kind: KindSessionMeta}); f.Seq != 24 {
		t.Fatalf("seq = %d, want 24", f.Seq)
	}
	if again := r.Journal("l2:x", ""); again != j {
		t.Fatal("registry returned a different journal")
	}
	if other := r.Journal("l2:y", ""); other.Head() != 0 {
		t.Fatalf("fresh journal head = %d", other.Head())
	}
}

func TestSessionKey(t *testing.T) {
	cases := map[string]string{"": "l1", "l1": "l1", "l1-session": "l1", "l2:abc": "l2:abc", "l2-abc-session": "l2:abc", "abc": "l2:abc"}
	for in, want := range cases {
		if got := SessionKey(in); got != want {
			t.Errorf("SessionKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// BenchmarkPublishContentDelta measures the cost the transcript adds to every
// streamed token: sequencing, view materialization and ring retention under
// the default bounds. It must stay far below the interval between tokens.
func BenchmarkPublishContentDelta(b *testing.B) {
	j := newJournal("l1", 1, DefaultConfig())
	j.Publish(Frame{Kind: KindRequestStarted, RequestID: "r"})
	j.Publish(Frame{Kind: KindInputCommitted, RequestID: "r", ItemID: "in:r", Text: "question"})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j.Publish(Frame{Kind: KindItemDelta, RequestID: "r", ItemID: "out:r:0", Channel: ChannelContent, Text: "token "})
	}
}
