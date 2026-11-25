// Package transcript owns the ordered, session-scoped event log that drives
// every live conversation view.
//
// Each logical session (l1 or l2:<uuid>) has one Journal. Every event that
// changes the visible conversation is published at the moment it is produced
// and receives a session-monotonic sequence number. Clients render items in
// sequence order, resume a stream from the last sequence they applied, and
// rebuild from a snapshot whose watermark is consistent with the log:
// applying the frames after a snapshot's watermark yields any later snapshot.
package transcript

import (
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

// Frame kinds. Every kind is delivered to every subscriber of the session.
const (
	KindInputQueued        = "input_queued"
	KindInputCommitted     = "input_committed"
	KindInputDropped       = "input_dropped"
	KindRequestStarted     = "request_started"
	KindRequestRoute       = "request_route"
	KindItemDelta          = "item_delta"
	KindItemSnapshot       = "item_snapshot"
	KindToolStarted        = "tool_started"
	KindToolFinished       = "tool_finished"
	KindDispatchTerminal   = "dispatch_terminal"
	KindDelegationStarted  = "delegation_started"
	KindDelegationFinished = "delegation_finished"
	KindItemDone           = "item_done"
	KindRequestEnd         = "request_end"
	KindCompactionStatus   = "compaction_status"
	KindSessionMeta        = "session_meta"
	KindTranscriptReset    = "transcript_reset"
)

// Delta channels carried by KindItemDelta.
const (
	ChannelContent   = "content"
	ChannelReasoning = "reasoning"
)

// Segment types rendered inside an item.
const (
	SegmentContent  = "content"
	SegmentThinking = "thinking"
	SegmentToolCall = "tool_call"
	SegmentCompact  = "compact"
	SegmentError    = "error"
)

// Item kinds.
const (
	ItemUser      = "user"
	ItemAssistant = "assistant"
	ItemError     = "error"
)

// File is an attachment reference shown with a user item.
type File struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// InputPart is one queued request inside an aggregated committed input.
type InputPart struct {
	RequestID string `json:"request_id"`
	ItemID    string `json:"item_id"`
	Text      string `json:"text"`
	TS        string `json:"ts,omitempty"`
}

// Segment is one rendered block inside an item.
type Segment struct {
	Type            string `json:"type"`
	Text            string `json:"text,omitempty"`
	CallID          string `json:"call_id,omitempty"`
	Name            string `json:"name,omitempty"`
	Args            string `json:"args,omitempty"`
	Result          string `json:"result,omitempty"`
	Error           string `json:"error,omitempty"`
	DurationMS      int64  `json:"duration_ms,omitempty"`
	Done            *bool  `json:"done,omitempty"`
	DispatchID      string `json:"dispatch_id,omitempty"`
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
}

// Frame is one ordered transcript event. Seq and TS are assigned by
// Journal.Publish; producers leave them empty.
type Frame struct {
	Seq  uint64 `json:"seq"`
	TS   string `json:"ts"`
	Kind string `json:"kind"`

	RequestID  string   `json:"request_id,omitempty"`
	RequestIDs []string `json:"request_ids,omitempty"`
	ItemID     string   `json:"item_id,omitempty"`

	// Input and delta payload.
	Text       string      `json:"text,omitempty"`
	Channel    string      `json:"channel,omitempty"`
	Parts      []InputPart `json:"parts,omitempty"`
	Files      []File      `json:"files,omitempty"`
	ImageCount int         `json:"image_count,omitempty"`
	ReceivedAt string      `json:"received_at,omitempty"`
	Origin     string      `json:"origin,omitempty"`

	// Tool, dispatch and delegation payload.
	CallID        string `json:"call_id,omitempty"`
	Name          string `json:"name,omitempty"`
	Args          string `json:"args,omitempty"`
	Result        string `json:"result,omitempty"`
	Error         string `json:"error,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	DispatchID    string `json:"dispatch_id,omitempty"`
	TargetAgentID string `json:"target_agent_id,omitempty"`
	AgentName     string `json:"agent_name,omitempty"`
	Revision      uint64 `json:"revision,omitempty"`
	Status        string `json:"status,omitempty"`
	NumTasks      int    `json:"num_tasks,omitempty"`

	// Whole-item replacement payload.
	Segments  []Segment `json:"segments,omitempty"`
	Ephemeral bool      `json:"ephemeral,omitempty"`

	// Request lifecycle payload.
	TaskType        string `json:"task_type,omitempty"`
	ModelID         string `json:"model_id,omitempty"`
	ProviderID      string `json:"provider_id,omitempty"`
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
	TerminalCode    string `json:"terminal_code,omitempty"`
	Reason          string `json:"reason,omitempty"`

	// Session metadata and compaction payload.
	SessionName string                            `json:"session_name,omitempty"`
	Plans       []string                          `json:"plans,omitempty"`
	Compaction  *timeline.CompactionStatusPayload `json:"compaction,omitempty"`
}

// Item is one ordered conversation entry. Items are ordered by (Seq, Sub).
// Seq is the sequence of the frame that opened the item; Sub orders items
// opened by the same frame (aggregated inputs) or persisted without a
// sequence (legacy rows, ordered after the preceding sequenced row).
type Item struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	RequestID  string    `json:"request_id,omitempty"`
	Seq        uint64    `json:"seq"`
	Sub        int       `json:"sub,omitempty"`
	LastSeq    uint64    `json:"last_seq,omitempty"`
	TS         string    `json:"ts"`
	Segments   []Segment `json:"segments"`
	Files      []File    `json:"files,omitempty"`
	ImageCount int       `json:"image_count,omitempty"`
	Done       bool      `json:"done,omitempty"`
	Ephemeral  bool      `json:"ephemeral,omitempty"`
}

// PendingInput is an input accepted by the session but not yet committed to
// the conversation (queued behind an active turn).
type PendingInput struct {
	RequestID  string `json:"request_id"`
	Text       string `json:"text"`
	Files      []File `json:"files,omitempty"`
	ImageCount int    `json:"image_count,omitempty"`
	ReceivedAt string `json:"received_at,omitempty"`
	Origin     string `json:"origin,omitempty"`
	Seq        uint64 `json:"seq"`
}

// RequestState is the lifecycle of one request as observed through frames.
type RequestState struct {
	RequestID       string   `json:"request_id"`
	RequestIDs      []string `json:"request_ids,omitempty"`
	Origin          string   `json:"origin,omitempty"`
	TaskType        string   `json:"task_type,omitempty"`
	ModelID         string   `json:"model_id,omitempty"`
	ProviderID      string   `json:"provider_id,omitempty"`
	AgentInstanceID string   `json:"agent_instance_id,omitempty"`
	Delegating      bool     `json:"delegating,omitempty"`
	Ended           bool     `json:"ended,omitempty"`
	TerminalCode    string   `json:"terminal_code,omitempty"`
	Error           string   `json:"error,omitempty"`
	StartedSeq      uint64   `json:"started_seq,omitempty"`
	EndedSeq        uint64   `json:"ended_seq,omitempty"`
}

// InputItemID is the stable identity of the user input owned by requestID.
func InputItemID(requestID string) string { return "in:" + requestID }

// OutputItemID is the stable identity of one assistant iteration.
func OutputItemID(requestID string, iter int) string {
	return fmt.Sprintf("out:%s:%d", requestID, iter)
}

// ErrorItemID is the stable identity of a request's terminal error.
func ErrorItemID(requestID string) string { return "err:" + requestID }

// SessionKey maps a session target ID to the logical session identity used by
// the Web Console (l1 or l2:<uuid>).
func SessionKey(targetID string) string {
	if targetID == "" || targetID == "l1" || targetID == "l1-session" {
		return "l1"
	}
	if strings.HasPrefix(targetID, "l2:") {
		return targetID
	}
	if strings.HasPrefix(targetID, "l2-") && strings.HasSuffix(targetID, "-session") {
		return "l2:" + strings.TrimSuffix(strings.TrimPrefix(targetID, "l2-"), "-session")
	}
	return "l2:" + targetID
}

func boolPtr(v bool) *bool { return &v }

func cloneSegments(in []Segment) []Segment {
	if in == nil {
		return nil
	}
	out := make([]Segment, len(in))
	for i, seg := range in {
		out[i] = seg
		if seg.Done != nil {
			out[i].Done = boolPtr(*seg.Done)
		}
	}
	return out
}

func cloneItem(in *Item) Item {
	out := *in
	out.Segments = cloneSegments(in.Segments)
	if in.Files != nil {
		out.Files = append([]File(nil), in.Files...)
	}
	return out
}

// Less reports whether item a sorts before item b.
func Less(a, b Item) bool {
	if a.Seq != b.Seq {
		return a.Seq < b.Seq
	}
	return a.Sub < b.Sub
}
