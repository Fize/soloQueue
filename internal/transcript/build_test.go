package transcript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

func msgEvent(ts string, m timeline.MessagePayload) timeline.Event {
	if m.Timestamp == "" {
		m.Timestamp = ts
	}
	return timeline.Event{Timestamp: ts, EventType: timeline.EventMessage, Message: &m}
}

func ctrlEvent(ts string, c timeline.ControlPayload) timeline.Event {
	return timeline.Event{Timestamp: ts, EventType: timeline.EventControl, Control: &c}
}

func ts(i int) string { return fmt.Sprintf("2026-01-01T00:00:%02dZ", i) }

func itemIDs(items []Item) string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return strings.Join(ids, ",")
}

func TestBuildKeepsLegacyGrouping(t *testing.T) {
	events := []timeline.Event{
		msgEvent(ts(0), timeline.MessagePayload{Role: "user", Content: "q1", RequestID: "r1"}),
		msgEvent(ts(1), timeline.MessagePayload{Role: "assistant", Content: "a", RequestID: "r1",
			ToolCalls: []timeline.ToolCallRec{{ID: "c1", Name: "echo"}}}),
		msgEvent(ts(2), timeline.MessagePayload{Role: "tool", Content: "done", ToolCallID: "c1", RequestID: "r1"}),
		msgEvent(ts(3), timeline.MessagePayload{Role: "user", Content: "q2", RequestID: "r2"}),
		msgEvent(ts(4), timeline.MessagePayload{Role: "assistant", Content: "b", RequestID: "r1"}),
		msgEvent(ts(5), timeline.MessagePayload{Role: "assistant", Content: "c", RequestID: "r2"}),
	}
	items := Build(events, BuildOptions{})
	if got := itemIDs(items); got != "legacy:0,legacy:1,legacy:3,legacy:5" {
		t.Fatalf("ids = %s", got)
	}
	merged := items[1]
	if len(merged.Segments) != 3 || merged.Segments[1].Result != "done" || merged.Segments[2].Text != "b" {
		t.Fatalf("merged legacy item = %#v", merged.Segments)
	}
}

func TestBuildOrdersStampedItemsBySequence(t *testing.T) {
	events := []timeline.Event{
		msgEvent(ts(0), timeline.MessagePayload{Role: "user", Content: "old", RequestID: "legacy"}),
		msgEvent(ts(1), timeline.MessagePayload{Role: "user", Content: "q1", RequestID: "r1", ItemID: "in:r1", Seq: 1}),
		msgEvent(ts(2), timeline.MessagePayload{Role: "assistant", Content: "a0", RequestID: "r1", ItemID: "out:r1:0", Seq: 2,
			ToolCalls: []timeline.ToolCallRec{{ID: "d1", Name: "delegate_x"}}}),
		msgEvent(ts(3), timeline.MessagePayload{Role: "tool", Content: "Delegation started: x", ToolCallID: "d1", RequestID: "r1"}),
		msgEvent(ts(4), timeline.MessagePayload{Role: "user", Content: "q2", RequestID: "r2", ItemID: "in:r2", Seq: 10}),
		// Committed after q2 but opened before it: ordered by its sequence.
		msgEvent(ts(5), timeline.MessagePayload{Role: "assistant", Content: "a1", RequestID: "r1", ItemID: "out:r1:1", Seq: 5}),
		msgEvent(ts(6), timeline.MessagePayload{Role: "user", IsEphemeral: true, RequestID: "r1",
			Content: "[Delegation Completed]\nTask: x\nCallID: d1\nResult:\nall good"}),
		msgEvent(ts(7), timeline.MessagePayload{Role: "assistant", Content: "a2", RequestID: "r2", ItemID: "out:r2:0", Seq: 20}),
		msgEvent(ts(8), timeline.MessagePayload{Role: "user", Content: "unstamped"}),
	}
	items := Build(events, BuildOptions{})
	if got := itemIDs(items); got != "legacy:0,in:r1,out:r1:0,out:r1:1,in:r2,out:r2:0,legacy:8" {
		t.Fatalf("ids = %s", got)
	}
	delegate := items[2].Segments[1]
	if delegate.Result != "all good" || delegate.Done == nil || !*delegate.Done {
		t.Fatalf("delegate = %#v", delegate)
	}
}

func TestBuildSplitsAggregatedInputAndDedupesFlush(t *testing.T) {
	events := []timeline.Event{
		msgEvent(ts(0), timeline.MessagePayload{Role: "user", Content: "one\n\ntwo", RequestID: "b", ItemID: "in:b", Seq: 7,
			TemporalParts: []timeline.TemporalPartPayload{{Content: "one", RequestID: "a", Timestamp: ts(0)}, {Content: "two", RequestID: "b", Timestamp: ts(1)}}}),
		msgEvent(ts(2), timeline.MessagePayload{Role: "assistant", Content: "x", RequestID: "b", ItemID: "out:b:0", Seq: 8,
			ToolCalls: []timeline.ToolCallRec{{ID: "c", Name: "t"}}}),
		msgEvent(ts(3), timeline.MessagePayload{Role: "assistant", Content: "x", RequestID: "b", ItemID: "out:b:0", Seq: 8}),
	}
	items := Build(events, BuildOptions{NormalizeUserText: strings.ToUpper})
	if got := itemIDs(items); got != "in:a,in:b,out:b:0" {
		t.Fatalf("ids = %s", got)
	}
	if items[0].Segments[0].Text != "ONE" || items[1].Sub != 1 || len(items[2].Segments) != 2 {
		t.Fatalf("items = %#v", items)
	}
}

func TestBuildHonorsRewindDeleteAndErrors(t *testing.T) {
	events := []timeline.Event{
		msgEvent(ts(0), timeline.MessagePayload{Role: "user", Content: "keep", RequestID: "r1", ItemID: "in:r1", Seq: 1}),
		msgEvent(ts(1), timeline.MessagePayload{Role: "user", Content: "deleted", RequestID: "r2", ItemID: "in:r2", Seq: 2}),
		msgEvent(ts(5), timeline.MessagePayload{Role: "user", Content: "rewound", RequestID: "r3", ItemID: "in:r3", Seq: 3}),
		ctrlEvent(ts(6), timeline.ControlPayload{Action: "delete", TargetTs: []string{ts(1)}}),
		ctrlEvent(ts(7), timeline.ControlPayload{Action: "rewind", TargetTs: []string{ts(4)}}),
		msgEvent(ts(8), timeline.MessagePayload{Role: "user", Content: "after", RequestID: "r4", ItemID: "in:r4", Seq: 4}),
		ctrlEvent(ts(9), timeline.ControlPayload{Action: "request_error", RequestID: "r4", ItemID: "err:r4", Content: "boom", Seq: 5}),
		ctrlEvent(ts(10), timeline.ControlPayload{Action: "summary", Reason: "auto_compact", Content: "hidden"}),
		ctrlEvent(ts(11), timeline.ControlPayload{Action: "summary", Reason: "user_command", Content: "sum", RequestID: "r5", ItemID: "out:r5:0"}),
	}
	items := Build(events, BuildOptions{})
	if got := itemIDs(items); got != "in:r1,in:r4,err:r4,out:r5:0" {
		t.Fatalf("ids = %s", got)
	}
	if items[2].Kind != ItemError || items[3].Segments[0].Type != SegmentCompact {
		t.Fatalf("items = %#v", items)
	}
}

func TestBuildAppliesDispatchOutcomes(t *testing.T) {
	events := []timeline.Event{
		msgEvent(ts(0), timeline.MessagePayload{Role: "assistant", RequestID: "r", ItemID: "out:r:0", Seq: 1,
			ToolCalls: []timeline.ToolCallRec{{ID: "d", Name: "delegate"}}}),
	}
	items := Build(events, BuildOptions{Dispatches: []DispatchOutcome{{RequestID: "r", ToolCallID: "d", DispatchID: "x", Status: "failed"}}})
	seg := items[0].Segments[0]
	if seg.Done == nil || !*seg.Done || seg.Error != "failed" || seg.DispatchID != "x" {
		t.Fatalf("segment = %#v", seg)
	}
}

func TestMergeAndPage(t *testing.T) {
	persisted := []Item{{ID: "a", Seq: 1}, {ID: "b", Seq: 2, Segments: []Segment{{Type: SegmentContent, Text: "old"}}}}
	inflight := []Item{{ID: "b", Seq: 2, Segments: []Segment{{Type: SegmentContent, Text: "new"}}}, {ID: "c", Seq: 3}}
	merged := Merge(persisted, inflight)
	if got := itemIDs(merged); got != "a,b,c" || merged[1].Segments[0].Text != "new" {
		t.Fatalf("merged = %#v", merged)
	}
	page, more, cursor := Page(merged, 2, "")
	if itemIDs(page) != "b,c" || !more || cursor != "b" {
		t.Fatalf("page = %s more=%v cursor=%q", itemIDs(page), more, cursor)
	}
	page, more, cursor = Page(merged, 2, cursor)
	if itemIDs(page) != "a" || more || cursor != "" {
		t.Fatalf("older page = %s more=%v cursor=%q", itemIDs(page), more, cursor)
	}
	if page, _, _ := Page(merged, 2, "a"); len(page) != 0 {
		t.Fatalf("before first = %s", itemIDs(page))
	}
}

func TestParseDelegationResultsAndFiles(t *testing.T) {
	got := ParseDelegationResults("[Delegation Completed]\nTask: a\nCallID: c1\nResult:\nline1\nline2\nTask: b\nCallID: c2\nResult:\nok")
	if got["c1"] != "line1\nline2" || got["c2"] != "ok" {
		t.Fatalf("results = %#v", got)
	}
	files := ExtractFiles("hi\n\n[Uploaded files:\n- File Name: a.png\n  Save Path: /tmp/a.png (Size: 3 bytes)\n]")
	if len(files) != 1 || files[0].Name != "a.png" || files[0].Path != "/tmp/a.png" {
		t.Fatalf("files = %#v", files)
	}
}

func TestExtractFilesFormats(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []File
	}{
		{"http format", "- File Name: a.png\n  Save Path: /p/a.png (Size: 123 bytes)", []File{{Name: "a.png", Path: "/p/a.png"}}},
		{"legacy ws image format", "- a.png: /p/a.png (image, recognized by visual model)", []File{{Name: "a.png", Path: "/p/a.png"}}},
		{"plain text without blocks", "just a normal message with no file blocks", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractFiles(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("ExtractFiles(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ExtractFiles(%q) = %#v, want %#v", tc.input, got, tc.want)
				}
			}
		})
	}
}
