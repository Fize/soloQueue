package transcript

import (
	"sort"
	"strings"
)

// View is a transcript materialized by applying frames in sequence order.
//
// The Journal keeps one View for the items of active requests so snapshots can
// include output that has not been persisted yet. Clients maintain the same
// state from a snapshot plus the frames after its watermark; the reducer in the
// Web Console mirrors Apply exactly.
type View struct {
	Items    map[string]*Item
	Pending  map[string]*PendingInput
	Requests map[string]*RequestState
	calls    map[string]string // request ID + "\x00" + call ID → item ID
	// dispatch identity is recorded per tool call so frames that arrive in
	// either order (tool_started / dispatch_started) end on the same segment.
	dispatch map[string]dispatchIdentity
	// tails accumulate the streaming text segment of open items, so a delta
	// costs its own length instead of a copy of the text so far.
	tails map[string]*textTail
}

// dispatchIdentity is the unique per-delegation identity of one tool call.
type dispatchIdentity struct {
	DispatchID      string
	AgentInstanceID string
}

// textTail is the builder behind an item's last text segment. A Builder only
// appends, so every string it returned earlier keeps its bytes and snapshots
// may share them.
type textTail struct {
	index int
	b     strings.Builder
}

// NewView returns an empty View.
func NewView() *View {
	return &View{
		Items:    make(map[string]*Item),
		Pending:  make(map[string]*PendingInput),
		Requests: make(map[string]*RequestState),
		calls:    make(map[string]string),
		dispatch: make(map[string]dispatchIdentity),
		tails:    make(map[string]*textTail),
	}
}

// appendTail appends text to the item's segment at index and returns the new
// segment text. The builder is reseeded when the segment changed by any other
// means.
func (v *View) appendTail(itemID string, index int, current, text string) string {
	t := v.tails[itemID]
	if t == nil || t.index != index || t.b.Len() != len(current) {
		t = &textTail{index: index}
		t.b.Grow(len(current) + len(text))
		t.b.WriteString(current)
		v.tails[itemID] = t
	}
	t.b.WriteString(text)
	return t.b.String()
}

// dropTails releases the builders of items that are finished or gone.
func (v *View) dropTails() {
	for id := range v.tails {
		if item, ok := v.Items[id]; !ok || item.Done {
			delete(v.tails, id)
		}
	}
}

// ViewFromSnapshot seeds a View with snapshot state.
func ViewFromSnapshot(items []Item, pending []PendingInput, requests []RequestState) *View {
	v := NewView()
	for i := range items {
		item := cloneItem(&items[i])
		v.Items[item.ID] = &item
		v.indexCalls(&item)
	}
	for i := range pending {
		p := pending[i]
		v.Pending[p.RequestID] = &p
	}
	for i := range requests {
		r := requests[i]
		v.Requests[r.RequestID] = &r
	}
	return v
}

func callKey(requestID, callID string) string { return requestID + "\x00" + callID }

func (v *View) indexCalls(item *Item) {
	for _, seg := range item.Segments {
		if seg.Type == SegmentToolCall && seg.CallID != "" {
			v.calls[callKey(item.RequestID, seg.CallID)] = item.ID
		}
	}
}

// rememberDispatch records the delegation identity of one tool call so it can
// be painted onto the segment whichever frame arrives first.
func (v *View) rememberDispatch(requestID, callID, dispatchID, agentInstanceID string) {
	if callID == "" || (dispatchID == "" && agentInstanceID == "") {
		return
	}
	key := callKey(requestID, callID)
	ident := v.dispatch[key]
	if dispatchID != "" {
		ident.DispatchID = dispatchID
	}
	if agentInstanceID != "" {
		ident.AgentInstanceID = agentInstanceID
	}
	v.dispatch[key] = ident
}

// paintDispatch fills the identity of every segment of item matching callID.
// Values already on the segment win, so a repeated frame cannot rewrite them.
func (v *View) paintDispatch(item *Item, callID string) {
	if item == nil || callID == "" {
		return
	}
	ident := v.dispatch[callKey(item.RequestID, callID)]
	for i := range item.Segments {
		seg := &item.Segments[i]
		if seg.Type != SegmentToolCall || seg.CallID != callID {
			continue
		}
		if seg.DispatchID == "" {
			seg.DispatchID = ident.DispatchID
		}
		if seg.AgentInstanceID == "" {
			seg.AgentInstanceID = ident.AgentInstanceID
		}
	}
}

// ensureItem returns the item, opening it at frame f when absent. The second
// result reports whether the item was opened by this frame.
func (v *View) ensureItem(id, kind, requestID string, f Frame) (*Item, bool) {
	if item, ok := v.Items[id]; ok {
		return item, false
	}
	item := &Item{
		ID:        id,
		Kind:      kind,
		RequestID: requestID,
		Seq:       f.Seq,
		LastSeq:   f.Seq,
		TS:        f.TS,
		Segments:  []Segment{},
	}
	v.Items[id] = item
	return item, true
}

func (v *View) ensureRequest(requestID string) *RequestState {
	r, ok := v.Requests[requestID]
	if !ok {
		r = &RequestState{RequestID: requestID}
		v.Requests[requestID] = r
	}
	return r
}

func (v *View) callItem(f Frame) *Item {
	if f.ItemID != "" {
		if item, ok := v.Items[f.ItemID]; ok {
			return item
		}
	}
	if id, ok := v.calls[callKey(f.RequestID, f.CallID)]; ok {
		return v.Items[id]
	}
	return nil
}

// Apply applies one frame and returns the IDs of items it opened.
func (v *View) Apply(f Frame) []string {
	var opened []string
	switch f.Kind {
	case KindInputQueued:
		if f.RequestID == "" {
			return nil
		}
		v.Pending[f.RequestID] = &PendingInput{
			RequestID:  f.RequestID,
			Text:       f.Text,
			Files:      append([]File(nil), f.Files...),
			ImageCount: f.ImageCount,
			ReceivedAt: f.ReceivedAt,
			Origin:     f.Origin,
			Seq:        f.Seq,
		}
	case KindInputDropped:
		delete(v.Pending, f.RequestID)
		for _, id := range f.RequestIDs {
			delete(v.Pending, id)
		}
	case KindInputCommitted:
		delete(v.Pending, f.RequestID)
		for _, id := range f.RequestIDs {
			delete(v.Pending, id)
		}
		if len(f.Parts) > 0 {
			for i, part := range f.Parts {
				delete(v.Pending, part.RequestID)
				id := part.ItemID
				if id == "" {
					id = InputItemID(part.RequestID)
				}
				if _, exists := v.Items[id]; exists {
					continue
				}
				ts := part.TS
				if ts == "" {
					ts = f.ReceivedAt
				}
				if ts == "" {
					ts = f.TS
				}
				item := &Item{
					ID: id, Kind: ItemUser, RequestID: part.RequestID,
					Seq: f.Seq, Sub: i, LastSeq: f.Seq, TS: ts, Done: true,
					Segments: []Segment{{Type: SegmentContent, Text: part.Text}},
				}
				if i == len(f.Parts)-1 {
					item.Files = append([]File(nil), f.Files...)
					item.ImageCount = f.ImageCount
				}
				v.Items[id] = item
				opened = append(opened, id)
			}
			return opened
		}
		id := f.ItemID
		if id == "" {
			id = InputItemID(f.RequestID)
		}
		if _, exists := v.Items[id]; exists {
			return nil
		}
		ts := f.ReceivedAt
		if ts == "" {
			ts = f.TS
		}
		v.Items[id] = &Item{
			ID: id, Kind: ItemUser, RequestID: f.RequestID,
			Seq: f.Seq, LastSeq: f.Seq, TS: ts, Done: true,
			Segments:   []Segment{{Type: SegmentContent, Text: f.Text}},
			Files:      append([]File(nil), f.Files...),
			ImageCount: f.ImageCount,
		}
		opened = append(opened, id)
	case KindItemDelta:
		if f.ItemID == "" || f.Text == "" {
			return nil
		}
		item, created := v.ensureItem(f.ItemID, ItemAssistant, f.RequestID, f)
		if created {
			opened = append(opened, item.ID)
		}
		segType := SegmentContent
		if f.Channel == ChannelReasoning {
			segType = SegmentThinking
		}
		if n := len(item.Segments); n > 0 && item.Segments[n-1].Type == segType {
			item.Segments[n-1].Text = v.appendTail(item.ID, n-1, item.Segments[n-1].Text, f.Text)
		} else {
			item.Segments = append(item.Segments, Segment{Type: segType, Text: f.Text})
		}
		item.LastSeq = f.Seq
	case KindItemSnapshot:
		if f.ItemID == "" {
			return nil
		}
		item, created := v.ensureItem(f.ItemID, ItemAssistant, f.RequestID, f)
		if created {
			opened = append(opened, item.ID)
		}
		item.Segments = cloneSegments(f.Segments)
		if item.Segments == nil {
			item.Segments = []Segment{}
		}
		delete(v.tails, item.ID)
		item.Ephemeral = f.Ephemeral
		item.LastSeq = f.Seq
		v.indexCalls(item)
	case KindToolStarted:
		if f.ItemID == "" {
			return nil
		}
		item, created := v.ensureItem(f.ItemID, ItemAssistant, f.RequestID, f)
		if created {
			opened = append(opened, item.ID)
		}
		v.calls[callKey(f.RequestID, f.CallID)] = item.ID
		for i := range item.Segments {
			if item.Segments[i].Type == SegmentToolCall && item.Segments[i].CallID == f.CallID {
				item.Segments[i].Name = f.Name
				item.Segments[i].Args = f.Args
				if item.Segments[i].DispatchID == "" {
					item.Segments[i].DispatchID = f.DispatchID
				}
				if item.Segments[i].AgentInstanceID == "" {
					item.Segments[i].AgentInstanceID = f.TargetAgentID
				}
				v.rememberDispatch(f.RequestID, f.CallID, f.DispatchID, f.TargetAgentID)
				v.paintDispatch(item, f.CallID)
				item.LastSeq = f.Seq
				return opened
			}
		}
		item.Segments = append(item.Segments, Segment{
			Type: SegmentToolCall, CallID: f.CallID, Name: f.Name, Args: f.Args,
			Done: boolPtr(false), DispatchID: f.DispatchID, AgentInstanceID: f.TargetAgentID,
		})
		v.rememberDispatch(f.RequestID, f.CallID, f.DispatchID, f.TargetAgentID)
		v.paintDispatch(item, f.CallID)
		item.LastSeq = f.Seq
	case KindToolFinished:
		item := v.callItem(f)
		if item == nil {
			return nil
		}
		for i := range item.Segments {
			seg := &item.Segments[i]
			if seg.Type != SegmentToolCall || seg.CallID != f.CallID {
				continue
			}
			seg.Result = f.Result
			seg.Error = f.Error
			seg.DurationMS = f.DurationMS
			seg.Done = boolPtr(true)
			item.LastSeq = f.Seq
			return nil
		}
		item.Segments = append(item.Segments, Segment{
			Type: SegmentToolCall, CallID: f.CallID, Name: f.Name,
			Result: f.Result, Error: f.Error, DurationMS: f.DurationMS, Done: boolPtr(true),
		})
		v.calls[callKey(item.RequestID, f.CallID)] = item.ID
		item.LastSeq = f.Seq
	case KindDispatchStarted:
		v.rememberDispatch(f.RequestID, f.CallID, f.DispatchID, f.AgentInstanceID)
		if itemID, ok := v.calls[callKey(f.RequestID, f.CallID)]; ok {
			v.paintDispatch(v.Items[itemID], f.CallID)
			if item := v.Items[itemID]; item != nil {
				item.LastSeq = f.Seq
			}
		}
	case KindDispatchTerminal:
		v.rememberDispatch(f.RequestID, f.CallID, f.DispatchID, f.AgentInstanceID)
		item := v.callItem(f)
		if item == nil {
			return nil
		}
		v.paintDispatch(item, f.CallID)
		for i := range item.Segments {
			seg := &item.Segments[i]
			if seg.Type != SegmentToolCall {
				continue
			}
			if seg.DispatchID != "" && seg.DispatchID != f.DispatchID {
				continue
			}
			if seg.DispatchID == "" && seg.CallID != f.CallID {
				continue
			}
			seg.DispatchID = f.DispatchID
			seg.Done = boolPtr(true)
			if f.Error != "" {
				seg.Error = f.Error
			}
			item.LastSeq = f.Seq
		}
	case KindItemDone:
		if item, ok := v.Items[f.ItemID]; ok {
			item.Done = true
			item.LastSeq = f.Seq
			delete(v.tails, item.ID)
		}
	case KindDelegationStarted:
		if f.RequestID != "" {
			v.ensureRequest(f.RequestID).Delegating = true
		}
	case KindDelegationFinished:
		if f.RequestID != "" {
			v.ensureRequest(f.RequestID).Delegating = false
		}
	case KindRequestStarted:
		if f.RequestID == "" {
			return nil
		}
		r := v.ensureRequest(f.RequestID)
		r.Origin = f.Origin
		r.StartedSeq = f.Seq
	case KindRequestRoute:
		if f.RequestID == "" {
			return nil
		}
		r := v.ensureRequest(f.RequestID)
		r.TaskType = f.TaskType
		r.ModelID = f.ModelID
		r.ProviderID = f.ProviderID
		r.AgentInstanceID = f.AgentInstanceID
	case KindRequestEnd:
		if f.RequestID == "" {
			return nil
		}
		r := v.ensureRequest(f.RequestID)
		r.Ended = true
		r.Delegating = false
		r.TerminalCode = f.TerminalCode
		r.Error = f.Error
		r.EndedSeq = f.Seq
		r.RequestIDs = append([]string(nil), f.RequestIDs...)
		ended := map[string]bool{f.RequestID: true}
		for _, id := range f.RequestIDs {
			ended[id] = true
			delete(v.Pending, id)
		}
		for _, item := range v.Items {
			if item.Kind == ItemAssistant && ended[item.RequestID] {
				item.Done = true
			}
		}
		v.dropTails()
		if f.Error != "" {
			id := ErrorItemID(f.RequestID)
			if _, exists := v.Items[id]; !exists {
				v.Items[id] = &Item{
					ID: id, Kind: ItemError, RequestID: f.RequestID,
					Seq: f.Seq, LastSeq: f.Seq, TS: f.TS, Done: true,
					Segments: []Segment{{Type: SegmentError, Text: f.Error}},
				}
				opened = append(opened, id)
			}
		}
	case KindTranscriptReset:
		// The client resynchronizes from a snapshot. The server-side View keeps
		// only items of requests that are still running.
		for id, item := range v.Items {
			if r, ok := v.Requests[item.RequestID]; !ok || r.Ended {
				delete(v.Items, id)
			}
		}
		for key, id := range v.calls {
			if _, ok := v.Items[id]; !ok {
				delete(v.calls, key)
			}
		}
		v.dropTails()
	}
	return opened
}

// RemoveRequests drops every item and request entry owned by the given IDs.
func (v *View) RemoveRequests(ids map[string]bool) {
	for id, item := range v.Items {
		if ids[item.RequestID] {
			delete(v.Items, id)
		}
	}
	for key, id := range v.calls {
		if _, ok := v.Items[id]; !ok {
			delete(v.calls, key)
		}
	}
	for id := range ids {
		delete(v.Requests, id)
	}
	v.dropTails()
}

// List returns copies of every item in transcript order.
func (v *View) List() []Item {
	out := make([]Item, 0, len(v.Items))
	for _, item := range v.Items {
		out = append(out, cloneItem(item))
	}
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
	return out
}

// PendingList returns pending inputs in arrival order.
func (v *View) PendingList() []PendingInput {
	out := make([]PendingInput, 0, len(v.Pending))
	for _, p := range v.Pending {
		cp := *p
		cp.Files = append([]File(nil), p.Files...)
		out = append(out, cp)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// RequestList returns request states ordered by start.
func (v *View) RequestList() []RequestState {
	out := make([]RequestState, 0, len(v.Requests))
	for _, r := range v.Requests {
		cp := *r
		cp.RequestIDs = append([]string(nil), r.RequestIDs...)
		out = append(out, cp)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedSeq != out[j].StartedSeq {
			return out[i].StartedSeq < out[j].StartedSeq
		}
		return out[i].RequestID < out[j].RequestID
	})
	return out
}
