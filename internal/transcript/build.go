package transcript

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

// legacySubBase orders rows persisted without a sequence after the sequenced
// row that precedes them in the timeline.
const legacySubBase = 1 << 20

// DispatchOutcome is an authoritative delegation result recorded outside the
// timeline (dispatch records outlive a parent stream that ends early).
type DispatchOutcome struct {
	RequestID  string
	ToolCallID string
	DispatchID string
	Status     string
}

// BuildOptions customizes Build.
type BuildOptions struct {
	// NormalizeUserText strips internal annotations from user input.
	NormalizeUserText func(string) string
	// Dispatches marks delegate tool calls with terminal dispatch outcomes.
	Dispatches []DispatchOutcome
}

// PostClear returns the events after the most recent clear control event and
// the time of that clear (zero when there was none).
func PostClear(events []timeline.Event) ([]timeline.Event, time.Time) {
	last := -1
	for i, evt := range events {
		if evt.EventType == timeline.EventControl && evt.Control != nil && evt.Control.Action == "clear" {
			last = i
		}
	}
	if last < 0 {
		return events, time.Time{}
	}
	clearAt, _ := time.Parse(time.RFC3339Nano, events[last].Timestamp)
	return events[last+1:], clearAt
}

type pendingCall struct {
	callID    string
	requestID string
	name      string
	itemID    string
}

type builder struct {
	opts      BuildOptions
	items     map[string]*Item
	order     []string
	calls     []pendingCall
	maxSeq    uint64
	legacySub int
}

func (b *builder) normalize(text string) string {
	if b.opts.NormalizeUserText != nil {
		return b.opts.NormalizeUserText(text)
	}
	return text
}

// place assigns an order key: the stamped sequence when present, otherwise a
// position after the latest sequenced row seen so far.
func (b *builder) place(item *Item, seq uint64, sub int) {
	if seq > 0 {
		item.Seq = seq
		item.Sub = sub
		if seq > b.maxSeq {
			b.maxSeq = seq
		}
		return
	}
	b.legacySub++
	item.Seq = b.maxSeq
	item.Sub = legacySubBase + b.legacySub
}

func (b *builder) add(item *Item) {
	b.items[item.ID] = item
	b.order = append(b.order, item.ID)
}

// Build converts post-clear timeline events into ordered items. Rows stamped
// with an item ID keep that identity; legacy rows receive stable positional
// IDs and keep their former grouping.
func Build(events []timeline.Event, opts BuildOptions) []Item {
	b := &builder{opts: opts, items: make(map[string]*Item)}

	// A rewind only hides messages that precede the control event; deletes
	// hide exact message timestamps.
	validThreshold := ""
	hiddenByRewind := make(map[int]bool)
	deleted := make(map[string]bool)
	for i := len(events) - 1; i >= 0; i-- {
		evt := events[i]
		if evt.EventType == timeline.EventControl && evt.Control != nil {
			switch evt.Control.Action {
			case "rewind":
				if len(evt.Control.TargetTs) > 0 {
					ts := evt.Control.TargetTs[0]
					if validThreshold == "" || ts < validThreshold {
						validThreshold = ts
					}
				}
			case "delete":
				for _, ts := range evt.Control.TargetTs {
					deleted[ts] = true
				}
			}
			continue
		}
		if evt.EventType == timeline.EventMessage && evt.Message != nil && validThreshold != "" {
			ts := evt.Message.Timestamp
			if ts == "" {
				ts = evt.Timestamp
			}
			if ts >= validThreshold {
				hiddenByRewind[i] = true
			}
		}
	}

	// Legacy assistant grouping state, kept identical to the former history
	// view: rows of one request merge into its first assistant item.
	var lastLegacy *Item
	for idx, evt := range events {
		if evt.EventType == timeline.EventControl && evt.Control != nil {
			ctrl := evt.Control
			switch ctrl.Action {
			case "summary":
				// Automatic compaction is an internal persistence boundary; only
				// summaries produced by an explicit command are visible.
				if ctrl.Reason == "auto_compact" {
					continue
				}
				id := ctrl.ItemID
				if id == "" {
					id = fmt.Sprintf("legacy:%d", idx)
				}
				item := &Item{ID: id, Kind: ItemAssistant, RequestID: ctrl.RequestID, TS: evt.Timestamp, Done: true,
					Segments: []Segment{{Type: SegmentCompact, Text: ctrl.Content}}}
				if existing, ok := b.items[id]; ok {
					existing.Segments = item.Segments
					continue
				}
				b.place(item, ctrl.Seq, 0)
				b.add(item)
				lastLegacy = nil
			case "request_error":
				if ctrl.RequestID == "" || ctrl.Content == "" {
					continue
				}
				id := ErrorItemID(ctrl.RequestID)
				if _, ok := b.items[id]; ok {
					continue
				}
				item := &Item{ID: id, Kind: ItemError, RequestID: ctrl.RequestID, TS: evt.Timestamp, Done: true,
					Segments: []Segment{{Type: SegmentError, Text: ctrl.Content}}}
				b.place(item, ctrl.Seq, 0)
				b.add(item)
			}
			continue
		}
		if evt.EventType != timeline.EventMessage || evt.Message == nil {
			continue
		}
		msg := evt.Message
		if msg.Role == "system" {
			continue
		}
		// Ephemeral non-tool messages are internal, except delegation results
		// which complete delegate tool calls.
		if msg.Role != "tool" && msg.IsEphemeral && !strings.HasPrefix(msg.Content, "[Delegation Completed]") {
			continue
		}
		ts := msg.Timestamp
		if ts == "" {
			ts = evt.Timestamp
		}
		if hiddenByRewind[idx] || deleted[ts] {
			continue
		}

		switch msg.Role {
		case "user":
			if strings.HasPrefix(msg.Content, "[Delegation Completed]") {
				results := ParseDelegationResults(msg.Content)
				for _, call := range b.calls {
					if !IsDelegationTool(call.name) {
						continue
					}
					result, ok := results[call.callID]
					if !ok || !RequestIDsMatch(call.requestID, msg.RequestID) {
						continue
					}
					b.updateCall(call, func(seg *Segment) {
						seg.Result = result
						seg.Done = boolPtr(true)
					})
				}
				continue
			}
			b.addUser(msg, ts, idx)
			lastLegacy = nil
		case "assistant":
			if msg.ItemID != "" {
				b.addStampedAssistant(msg, ts)
				lastLegacy = nil
				continue
			}
			lastLegacy = b.addLegacyAssistant(msg, ts, idx, lastLegacy)
		case "tool":
			for _, call := range b.calls {
				if call.callID != msg.ToolCallID || !RequestIDsMatch(call.requestID, msg.RequestID) {
					continue
				}
				content := msg.Content
				b.updateCall(call, func(seg *Segment) {
					if IsDelegationTool(call.name) && (content == "" || strings.HasPrefix(content, "Delegation started:")) {
						// The first delegate result is a placeholder; the call
						// completes with the delegation result message.
						seg.Done = boolPtr(false)
						return
					}
					seg.Result = content
					seg.Done = boolPtr(true)
				})
				break
			}
		}
	}

	for _, outcome := range opts.Dispatches {
		if outcome.Status == "running" || outcome.Status == "persistence_pending" {
			continue
		}
		for _, call := range b.calls {
			if !IsDelegationTool(call.name) || call.callID != outcome.ToolCallID || !RequestIDsMatch(call.requestID, outcome.RequestID) {
				continue
			}
			status := outcome.Status
			dispatchID := outcome.DispatchID
			b.updateCall(call, func(seg *Segment) {
				seg.Done = boolPtr(true)
				seg.DispatchID = dispatchID
				if status != "completed" {
					seg.Error = status
				}
			})
		}
	}

	out := make([]Item, 0, len(b.order))
	for _, id := range b.order {
		if item, ok := b.items[id]; ok {
			out = append(out, cloneItem(item))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
	return out
}

func (b *builder) updateCall(call pendingCall, fn func(*Segment)) {
	item, ok := b.items[call.itemID]
	if !ok {
		return
	}
	for i := range item.Segments {
		if item.Segments[i].Type == SegmentToolCall && item.Segments[i].CallID == call.callID {
			fn(&item.Segments[i])
			return
		}
	}
}

func (b *builder) addUser(msg *timeline.MessagePayload, ts string, idx int) {
	files := make([]File, 0, len(msg.Files))
	for _, f := range msg.Files {
		files = append(files, File{Name: f.Name, Path: f.Path})
	}
	if len(files) == 0 {
		files = ExtractFiles(msg.Content)
	}
	stamped := msg.ItemID != "" || msg.Seq > 0
	if stamped && len(msg.TemporalParts) > 1 && partsHaveRequestIDs(msg.TemporalParts) {
		for i, part := range msg.TemporalParts {
			id := InputItemID(part.RequestID)
			if _, ok := b.items[id]; ok {
				continue
			}
			partTS := part.Timestamp
			if partTS == "" {
				partTS = ts
			}
			item := &Item{ID: id, Kind: ItemUser, RequestID: part.RequestID, TS: partTS, Done: true,
				Segments: []Segment{{Type: SegmentContent, Text: b.normalize(part.Content)}}}
			if i == len(msg.TemporalParts)-1 {
				item.Files = files
			}
			b.place(item, msg.Seq, i)
			b.add(item)
		}
		return
	}
	if stamped {
		id := msg.ItemID
		if id == "" {
			id = InputItemID(msg.RequestID)
		}
		if _, ok := b.items[id]; ok {
			return
		}
		item := &Item{ID: id, Kind: ItemUser, RequestID: msg.RequestID, TS: ts, Done: true, Files: files,
			Segments: []Segment{{Type: SegmentContent, Text: b.normalize(msg.Content)}}}
		b.place(item, msg.Seq, 0)
		b.add(item)
		return
	}

	// Legacy rows: a repeated user row of the same request with identical text
	// within five seconds is a duplicate write.
	text := b.normalize(msg.Content)
	if n := len(b.order); n > 0 {
		if last := b.items[b.order[n-1]]; last != nil && last.Kind == ItemUser && last.RequestID == msg.RequestID &&
			len(last.Segments) == 1 && last.Segments[0].Type == SegmentContent && last.Segments[0].Text == text {
			t1, err1 := time.Parse(time.RFC3339Nano, last.TS)
			t2, err2 := time.Parse(time.RFC3339Nano, ts)
			if err1 == nil && err2 == nil && t2.Sub(t1) < 5*time.Second {
				return
			}
		}
	}
	segments := []Segment{}
	if msg.Content != "" {
		segments = append(segments, Segment{Type: SegmentContent, Text: text})
	}
	item := &Item{ID: fmt.Sprintf("legacy:%d", idx), Kind: ItemUser, RequestID: msg.RequestID, TS: ts, Done: true,
		Files: files, Segments: segments}
	b.place(item, 0, 0)
	b.add(item)
}

func partsHaveRequestIDs(parts []timeline.TemporalPartPayload) bool {
	for _, part := range parts {
		if part.RequestID == "" {
			return false
		}
	}
	return true
}

// assistantSegments renders one persisted assistant row in its recorded order.
func (b *builder) assistantSegments(msg *timeline.MessagePayload, itemID string) []Segment {
	var segments []Segment
	reasoningAdded, contentAdded, toolsAdded := false, false, false
	appendReasoning := func() {
		if reasoningAdded || msg.ReasoningContent == "" {
			return
		}
		reasoningAdded = true
		segments = append(segments, Segment{Type: SegmentThinking, Text: msg.ReasoningContent})
	}
	appendContent := func() {
		if contentAdded || msg.Content == "" {
			return
		}
		contentAdded = true
		segments = append(segments, Segment{Type: SegmentContent, Text: msg.Content})
	}
	appendTools := func() {
		if toolsAdded {
			return
		}
		toolsAdded = true
		for _, tc := range msg.ToolCalls {
			segments = append(segments, Segment{Type: SegmentToolCall, CallID: tc.ID, Name: tc.Name, Args: tc.Arguments, Done: boolPtr(false)})
			b.calls = append(b.calls, pendingCall{callID: tc.ID, requestID: msg.RequestID, name: tc.Name, itemID: itemID})
		}
	}
	for _, part := range msg.SegmentOrder {
		switch part {
		case "reasoning":
			appendReasoning()
		case "content":
			appendContent()
		case "tool_calls":
			appendTools()
		}
	}
	appendReasoning()
	appendContent()
	appendTools()
	return segments
}

// duplicateFlush reports whether a content-only row repeats the latest content
// of an item whose previous row carried tool calls: a cancelled or terminated
// turn can persist the same text twice.
func duplicateFlush(item *Item, msg *timeline.MessagePayload) bool {
	if len(msg.ToolCalls) > 0 || msg.Content == "" {
		return false
	}
	hasTool := false
	for _, seg := range item.Segments {
		if seg.Type == SegmentToolCall {
			hasTool = true
			break
		}
	}
	if !hasTool {
		return false
	}
	for i := len(item.Segments) - 1; i >= 0; i-- {
		if item.Segments[i].Type == SegmentContent {
			return item.Segments[i].Text == msg.Content
		}
	}
	return false
}

func (b *builder) addStampedAssistant(msg *timeline.MessagePayload, ts string) {
	if item, ok := b.items[msg.ItemID]; ok {
		if duplicateFlush(item, msg) {
			return
		}
		for _, seg := range item.Segments {
			if seg.Type == SegmentContent && seg.Text == msg.Content && len(msg.ToolCalls) == 0 {
				return
			}
		}
		item.Segments = append(item.Segments, b.assistantSegments(msg, item.ID)...)
		return
	}
	item := &Item{ID: msg.ItemID, Kind: ItemAssistant, RequestID: msg.RequestID, TS: ts, Done: true}
	item.Segments = b.assistantSegments(msg, item.ID)
	if item.Segments == nil {
		item.Segments = []Segment{}
	}
	b.place(item, msg.Seq, 0)
	b.add(item)
}

func (b *builder) addLegacyAssistant(msg *timeline.MessagePayload, ts string, idx int, lastLegacy *Item) *Item {
	var target *Item
	if msg.RequestID != "" {
		for i := len(b.order) - 1; i >= 0; i-- {
			candidate := b.items[b.order[i]]
			if candidate == nil {
				continue
			}
			// Older channel queues lost request ownership: a user row without
			// a request ID always starts a new reply.
			if candidate.Kind == ItemUser && candidate.RequestID == "" {
				break
			}
			if candidate.Kind == ItemAssistant && strings.HasPrefix(candidate.ID, "legacy:") && candidate.RequestID == msg.RequestID {
				target = candidate
				break
			}
		}
	} else if lastLegacy != nil && lastLegacy.RequestID == "" {
		target = lastLegacy
	}
	if target != nil {
		if duplicateFlush(target, msg) {
			return target
		}
		target.Segments = append(target.Segments, b.assistantSegments(msg, target.ID)...)
		return target
	}
	item := &Item{ID: fmt.Sprintf("legacy:%d", idx), Kind: ItemAssistant, RequestID: msg.RequestID, TS: ts, Done: true}
	item.Segments = b.assistantSegments(msg, item.ID)
	if item.Segments == nil {
		item.Segments = []Segment{}
	}
	b.place(item, 0, 0)
	b.add(item)
	return item
}

// Assemble combines persisted items read after snap was taken with the
// snapshot's in-memory items. A sequenced item that opened after the
// watermark is dropped even if it is already persisted: the frames after the
// watermark rebuild it, and keeping it would apply those frames twice.
func Assemble(persisted []Item, snap Snapshot) []Item {
	kept := persisted[:0:0]
	for _, item := range persisted {
		if item.Sub < legacySubBase && item.Seq > snap.Seq {
			continue
		}
		kept = append(kept, item)
	}
	return Merge(kept, snap.Items)
}

// Merge overlays in-memory items on persisted ones. The in-memory copy wins:
// it includes output and tool results that may not be persisted yet.
func Merge(persisted, inflight []Item) []Item {
	byID := make(map[string]int, len(persisted))
	out := make([]Item, 0, len(persisted)+len(inflight))
	for _, item := range persisted {
		byID[item.ID] = len(out)
		out = append(out, item)
	}
	for _, item := range inflight {
		if i, ok := byID[item.ID]; ok {
			out[i] = item
			continue
		}
		byID[item.ID] = len(out)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
	return out
}

// Page returns at most limit items ending before the item with ID before (or
// the newest items when before is empty), whether older items exist, and the
// cursor of the oldest returned item.
func Page(items []Item, limit int, before string) ([]Item, bool, string) {
	end := len(items)
	if before != "" {
		end = -1
		for i, item := range items {
			if item.ID == before {
				end = i
				break
			}
		}
		if end <= 0 {
			return []Item{}, false, ""
		}
	}
	start := 0
	if limit > 0 && end-limit > 0 {
		start = end - limit
	}
	page := items[start:end]
	if len(page) == 0 {
		return []Item{}, false, ""
	}
	cursor := ""
	if start > 0 {
		cursor = page[0].ID
	}
	return page, start > 0, cursor
}

// IsDelegationTool reports whether a tool call delegates to another agent.
func IsDelegationTool(name string) bool {
	return name == "delegate" || strings.HasPrefix(name, "delegate_")
}

// RequestIDsMatch keeps legacy rows (no request ID) matching while preventing
// two identified requests that reuse a tool call ID from sharing results.
func RequestIDsMatch(left, right string) bool {
	return left == "" || right == "" || left == right
}

// ParseDelegationResults parses callID→result pairs from a
// "[Delegation Completed]" message.
func ParseDelegationResults(content string) map[string]string {
	results := make(map[string]string)
	var currentCallID string
	var resultLines []string
	inResult := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Task:"):
			if currentCallID != "" && inResult {
				results[currentCallID] = strings.TrimSpace(strings.Join(resultLines, "\n"))
			}
			currentCallID = ""
			resultLines = nil
			inResult = false
		case strings.HasPrefix(trimmed, "CallID:"):
			currentCallID = strings.TrimSpace(strings.TrimPrefix(trimmed, "CallID:"))
		case trimmed == "Result:":
			inResult = true
		case inResult:
			resultLines = append(resultLines, line)
		}
	}
	if currentCallID != "" && inResult {
		results[currentCallID] = strings.TrimSpace(strings.Join(resultLines, "\n"))
	}
	return results
}

var fileBlockRegex = regexp.MustCompile(`- File Name:\s*(.+?)\n\s*Save Path:\s*(.+?)\s+\(Size:`)

// legacyFileBlockRegex matches the old upload block format
// `- name: path (image, recognized by visual model)`.
var legacyFileBlockRegex = regexp.MustCompile(`- ([^:\n]+?):\s*(\S+?)\s+\(image, recognized by visual model\)`)

// ExtractFiles parses attachment blocks embedded in a prompt.
func ExtractFiles(content string) []File {
	var files []File
	for _, m := range fileBlockRegex.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			files = append(files, File{Name: strings.TrimSpace(m[1]), Path: strings.TrimSpace(m[2])})
		}
	}
	for _, m := range legacyFileBlockRegex.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			files = append(files, File{Name: strings.TrimSpace(m[1]), Path: strings.TrimSpace(m[2])})
		}
	}
	return files
}
