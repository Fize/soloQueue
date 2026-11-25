package session

import (
	"context"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// SetTranscriptRegistry selects the registry that owns this session's
// journal. A session without a registry neither publishes frames nor stamps
// rows; cron and other headless sessions run that way.
func (s *Session) SetTranscriptRegistry(r *transcript.Registry) {
	s.transcriptReg.Store(r)
	s.journal.Store(nil)
}

// TranscriptKey is the logical session identity (l1 or l2:<uuid>).
func (s *Session) TranscriptKey() string { return transcript.SessionKey(s.TargetID) }

// Transcript returns the session's ordered event log, or nil when no
// registry is installed.
func (s *Session) Transcript() *transcript.Journal { return s.transcriptJournal() }

func (s *Session) transcriptJournal() *transcript.Journal {
	if j := s.journal.Load(); j != nil {
		return j
	}
	reg := s.transcriptReg.Load()
	if reg == nil {
		return nil
	}
	dir := ""
	if s.tl != nil {
		dir = s.tl.Dir()
	}
	j := reg.Journal(s.TranscriptKey(), dir)
	s.journal.CompareAndSwap(nil, j)
	return s.journal.Load()
}

// publishTranscript publishes f and returns it with its sequence; without a
// journal it returns f unchanged (Seq 0).
func (s *Session) publishTranscript(f transcript.Frame) transcript.Frame {
	j := s.transcriptJournal()
	if j == nil {
		return f
	}
	return j.Publish(f)
}

func formatTranscriptTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func transcriptFilesFrom(files []ctxwin.FileAttachment, text string) []transcript.File {
	out := make([]transcript.File, 0, len(files))
	for _, f := range files {
		out = append(out, transcript.File{Name: f.Name, Path: f.Path})
	}
	if len(out) == 0 {
		out = transcript.ExtractFiles(text)
	}
	return out
}

// stampPush binds every visible ContextWindow push to its live transcript
// item. It runs inside ContextWindow.Push before the message is stored or
// persisted, so the timeline row always carries the item identity and the
// sequence at which the item opened.
func (s *Session) stampPush(msg *ctxwin.Message) {
	if msg.ItemID != "" {
		return
	}
	j := s.transcriptJournal()
	if j == nil {
		return
	}
	switch msg.Role {
	case ctxwin.RoleUser:
		if msg.IsSummary || msg.IsPersonaState || msg.IsEphemeral || msg.RequestID == "" {
			return
		}
		frame := transcript.Frame{
			Kind:       transcript.KindInputCommitted,
			RequestID:  msg.RequestID,
			ItemID:     transcript.InputItemID(msg.RequestID),
			ReceivedAt: formatTranscriptTS(msg.Timestamp),
			Files:      transcriptFilesFrom(msg.Files, msg.Content),
			ImageCount: len(msg.Images),
		}
		if parts := msg.TemporalParts; len(parts) > 1 && temporalPartsOwned(parts) {
			for _, part := range parts {
				frame.Parts = append(frame.Parts, transcript.InputPart{
					RequestID: part.RequestID,
					ItemID:    transcript.InputItemID(part.RequestID),
					Text:      StripRecalledMemories(part.Content),
					TS:        formatTranscriptTS(part.Timestamp),
				})
				frame.RequestIDs = append(frame.RequestIDs, part.RequestID)
			}
		} else {
			frame.Text = StripRecalledMemories(msg.Content)
			for _, part := range parts {
				if part.RequestID != "" && part.RequestID != msg.RequestID {
					frame.RequestIDs = append(frame.RequestIDs, part.RequestID)
				}
			}
		}
		published := j.Publish(frame)
		msg.ItemID = frame.ItemID
		msg.Seq = published.Seq
	case ctxwin.RoleAssistant:
		if msg.RequestID == "" || !msg.IterSet {
			return
		}
		itemID := transcript.OutputItemID(msg.RequestID, msg.Iter)
		seq, openedAt, ok := j.ItemOpened(itemID)
		if !ok {
			// The iteration produced no live frame (for example its tool
			// calls never started). Publish the row so the persisted item
			// has a live counterpart at the same position.
			j.Publish(transcript.Frame{
				Kind:      transcript.KindItemSnapshot,
				RequestID: msg.RequestID,
				ItemID:    itemID,
				Segments:  segmentsFromMessage(msg),
			})
			seq, openedAt, ok = j.ItemOpened(itemID)
			if !ok {
				return
			}
		}
		msg.ItemID = itemID
		msg.Seq = seq
		if !openedAt.IsZero() {
			msg.Timestamp = openedAt
		}
	}
}

func temporalPartsOwned(parts []ctxwin.TemporalPart) bool {
	for _, part := range parts {
		if part.RequestID == "" {
			return false
		}
	}
	return true
}

func segmentsFromMessage(msg *ctxwin.Message) []transcript.Segment {
	var segments []transcript.Segment
	if msg.ReasoningContent != "" {
		segments = append(segments, transcript.Segment{Type: transcript.SegmentThinking, Text: msg.ReasoningContent})
	}
	if msg.Content != "" {
		segments = append(segments, transcript.Segment{Type: transcript.SegmentContent, Text: msg.Content})
	}
	for _, tc := range msg.ToolCalls {
		done := false
		segments = append(segments, transcript.Segment{
			Type: transcript.SegmentToolCall, CallID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments, Done: &done,
		})
	}
	return segments
}

// stampPartialFlush binds a partial assistant row written directly to the
// timeline (outside ContextWindow.Push) to its live item.
func (s *Session) stampPartialFlush(payload *timeline.MessagePayload, iter int, iterSet bool) {
	if payload.RequestID == "" || !iterSet {
		return
	}
	j := s.transcriptJournal()
	if j == nil {
		return
	}
	itemID := transcript.OutputItemID(payload.RequestID, iter)
	seq, openedAt, ok := j.ItemOpened(itemID)
	if !ok {
		return
	}
	payload.ItemID = itemID
	payload.Seq = seq
	if !openedAt.IsZero() {
		payload.Timestamp = formatTranscriptTS(openedAt)
	}
}

// transcriptSink publishes the leader agent's events for one request. Events
// from any other agent (delegated children, helpers sharing the request
// context) are ignored.
type transcriptSink struct {
	session   *Session
	requestID string
	mu        sync.RWMutex
	owners    map[string]bool
}

func (s *Session) newTranscriptSink(owner, requestID string) *transcriptSink {
	t := &transcriptSink{session: s, requestID: requestID, owners: make(map[string]bool)}
	t.allow(owner)
	return t
}

// allow adds a leader instance, used when a quarantined leader is replaced
// while the request is in flight.
func (t *transcriptSink) allow(owner string) {
	if t == nil || owner == "" {
		return
	}
	t.mu.Lock()
	t.owners[owner] = true
	t.mu.Unlock()
}

func (t *transcriptSink) PublishAgentEvent(owner string, ev iface.AgentEvent) {
	t.mu.RLock()
	allowed := t.owners[owner]
	t.mu.RUnlock()
	if !allowed {
		return
	}
	agentEv, ok := ev.(agent.AgentEvent)
	if !ok {
		return
	}
	requestID := agent.RequestIDOfEvent(agentEv)
	if requestID == "" {
		requestID = t.requestID
	}
	if requestID == "" {
		return
	}
	j := t.session.transcriptJournal()
	if j == nil {
		return
	}
	switch e := agentEv.(type) {
	case agent.ContentDeltaEvent:
		if e.Delta == "" {
			return
		}
		j.Publish(transcript.Frame{Kind: transcript.KindItemDelta, RequestID: requestID,
			ItemID: transcript.OutputItemID(requestID, e.Iter), Channel: transcript.ChannelContent, Text: e.Delta})
	case agent.ReasoningDeltaEvent:
		if e.Delta == "" {
			return
		}
		j.Publish(transcript.Frame{Kind: transcript.KindItemDelta, RequestID: requestID,
			ItemID: transcript.OutputItemID(requestID, e.Iter), Channel: transcript.ChannelReasoning, Text: e.Delta})
	case agent.ToolExecStartEvent:
		j.Publish(transcript.Frame{Kind: transcript.KindToolStarted, RequestID: requestID,
			ItemID: transcript.OutputItemID(requestID, e.Iter), CallID: e.CallID, Name: e.Name, Args: e.Args,
			DispatchID: e.DispatchID, TargetAgentID: e.TargetAgentID})
	case agent.ToolExecDoneEvent:
		errText := ""
		if e.Err != nil {
			errText = e.Err.Error()
		}
		j.Publish(transcript.Frame{Kind: transcript.KindToolFinished, RequestID: requestID,
			ItemID: transcript.OutputItemID(requestID, e.Iter), CallID: e.CallID, Name: e.Name,
			Result: e.Result, Error: errText, DurationMS: e.Duration.Milliseconds()})
	case agent.IterationDoneEvent:
		itemID := transcript.OutputItemID(requestID, e.Iter)
		if _, _, open := j.ItemOpened(itemID); open {
			j.Publish(transcript.Frame{Kind: transcript.KindItemDone, RequestID: requestID, ItemID: itemID})
		}
	case agent.DelegationStartedEvent:
		j.Publish(transcript.Frame{Kind: transcript.KindDelegationStarted, RequestID: requestID,
			ItemID: transcript.OutputItemID(requestID, e.Iter), NumTasks: e.NumTasks})
	case agent.DelegationCompletedEvent:
		j.Publish(transcript.Frame{Kind: transcript.KindDelegationFinished, RequestID: requestID,
			ItemID: transcript.OutputItemID(requestID, e.Iter), TargetAgentID: e.TargetAgentID,
			AgentName: e.TargetAgentName, Result: e.ResultContent})
	}
}

// publishSyntheticOutput publishes output produced by the Session itself
// (slash commands) rather than by the leader agent.
func (s *Session) publishSyntheticOutput(requestID string, segments []transcript.Segment, ephemeral bool) {
	if requestID == "" {
		return
	}
	s.publishTranscript(transcript.Frame{
		Kind:      transcript.KindItemSnapshot,
		RequestID: requestID,
		ItemID:    transcript.OutputItemID(requestID, 0),
		Segments:  segments,
		Ephemeral: ephemeral,
	})
}

func (s *Session) publishInputQueued(requestID, prompt string, receivedAt time.Time, files []ctxwin.FileAttachment, images int, origin string) {
	if requestID == "" {
		return
	}
	s.publishTranscript(transcript.Frame{
		Kind:       transcript.KindInputQueued,
		RequestID:  requestID,
		Text:       StripRecalledMemories(prompt),
		Files:      transcriptFilesFrom(files, prompt),
		ImageCount: images,
		ReceivedAt: formatTranscriptTS(receivedAt),
		Origin:     origin,
	})
}

func (s *Session) publishInputDropped(reason string, requestIDs ...string) {
	ids := make([]string, 0, len(requestIDs))
	for _, id := range requestIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	s.publishTranscript(transcript.Frame{Kind: transcript.KindInputDropped, RequestID: ids[0], RequestIDs: ids, Reason: reason})
}

func (s *Session) publishRequestStarted(ctx context.Context, requestID string) {
	if requestID == "" {
		return
	}
	s.publishTranscript(transcript.Frame{
		Kind:      transcript.KindRequestStarted,
		RequestID: requestID,
		Origin:    telemetry.MetadataFromContext(ctx).Origin,
	})
}

func (s *Session) publishRequestRouteFrame(requestID string, route RequestRoute) {
	if requestID == "" {
		return
	}
	s.publishTranscript(transcript.Frame{
		Kind:            transcript.KindRequestRoute,
		RequestID:       requestID,
		TaskType:        route.TaskType,
		ModelID:         route.ModelID,
		ProviderID:      route.ProviderID,
		AgentInstanceID: route.AgentInstanceID,
	})
}

// publishRequestEnd closes a request after its final output is persisted. A
// displayable failure opens the request's error item, persisted as a
// request_error control row carrying the same identity and sequence.
func (s *Session) publishRequestEnd(requestID, code, message string, drained []string) {
	if requestID == "" {
		return
	}
	if code == "" {
		code = "completed"
	}
	ids := make([]string, 0, len(drained)+1)
	ids = append(ids, requestID)
	for _, id := range drained {
		if id != "" && id != requestID {
			ids = append(ids, id)
		}
	}
	errText := ""
	// A user stop and a plain context cancellation are not failures; the
	// stream never exposes them as errors either (see shouldExposeTerminal).
	if code != "completed" && code != "queued" && code != string(runwatch.CodeCancelledByUser) &&
		message != "" && message != context.Canceled.Error() {
		errText = message
	}
	frame := s.publishTranscript(transcript.Frame{
		Kind:         transcript.KindRequestEnd,
		RequestID:    requestID,
		RequestIDs:   ids,
		TerminalCode: code,
		Error:        errText,
	})
	if errText != "" && frame.Seq > 0 && s.tl != nil {
		_ = s.tl.AppendControl(&timeline.ControlPayload{
			Action:    "request_error",
			RequestID: requestID,
			ItemID:    transcript.ErrorItemID(requestID),
			Content:   errText,
			Seq:       frame.Seq,
		})
	}
}

func (s *Session) publishDispatchTerminal(record dispatch.Record) {
	status := string(record.Status)
	frame := transcript.Frame{
		Kind:       transcript.KindDispatchTerminal,
		RequestID:  record.RequestID,
		CallID:     record.ToolCallID,
		DispatchID: record.ID,
		Revision:   record.Revision,
		Status:     status,
	}
	if record.Status != dispatch.StatusCompleted {
		frame.Error = status
	}
	s.publishTranscript(frame)
}

// PublishTranscriptReset tells subscribers that persisted history changed
// outside the live stream (clear, rewind, delete) and must be reloaded.
func (s *Session) PublishTranscriptReset(reason string) {
	s.publishTranscript(transcript.Frame{Kind: transcript.KindTranscriptReset, Reason: reason})
}

// PublishSessionMeta publishes a session title or plan list change.
func (s *Session) PublishSessionMeta(name string, plans []string) {
	s.publishTranscript(transcript.Frame{Kind: transcript.KindSessionMeta, SessionName: name, Plans: plans})
}

func imageCount(ctx context.Context) int {
	images, _ := ctx.Value(ctxwin.ImageContextKey).([]llm.ImageContent)
	return len(images)
}

// compactionPayload is the persisted and published form of a compaction
// lifecycle change.
func compactionPayload(status ctxwin.CompactionStatus) timeline.CompactionStatusPayload {
	payload := timeline.CompactionStatusPayload{
		ID:        status.ID,
		Status:    status.Status,
		Reason:    status.Reason,
		StartedAt: status.StartedAt.Format(time.RFC3339Nano),
		Error:     status.Error,
	}
	if !status.FinishedAt.IsZero() {
		payload.FinishedAt = status.FinishedAt.Format(time.RFC3339Nano)
	}
	return payload
}

// publishCompactionStatus observes this session's context window. It runs
// after the builder's hook has persisted the status row, so a snapshot that
// covers the frame also reads the row.
func (s *Session) publishCompactionStatus(_ context.Context, status ctxwin.CompactionStatus) {
	payload := compactionPayload(status)
	s.publishTranscript(transcript.Frame{Kind: transcript.KindCompactionStatus, Compaction: &payload})
}
