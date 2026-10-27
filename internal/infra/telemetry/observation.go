package telemetry

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/observability"
)

type callRecordKey struct{}
type callRecord struct {
	call                           observability.Call
	store                          *observability.Store
	observer                       *llm.CallObserver
	done                           atomic.Bool
	content, reasoning             string
	contentBuffer, reasoningBuffer strings.Builder
	checkpointAt                   time.Time
	checkpointBytes                int
	truncated                      bool
}

func (r *callRecord) finish() bool { return r.done.CompareAndSwap(false, true) }

// Bound capture even when a provider ignores cancellation or returns unexpectedly large chunks.
func (r *callRecord) appendContent(s string)   { r.append(&r.contentBuffer, s) }
func (r *callRecord) appendReasoning(s string) { r.append(&r.reasoningBuffer, s) }
func (r *callRecord) append(dst *strings.Builder, s string) {
	remaining := (8 << 20) - r.contentBuffer.Len() - r.reasoningBuffer.Len()
	if remaining <= 0 {
		r.truncated = true
		return
	}
	if len(s) > remaining {
		s = s[:remaining]
		r.truncated = true
	}
	dst.WriteString(s)
	if r.store != nil && time.Since(r.checkpointAt) >= time.Second && r.contentBuffer.Len()+r.reasoningBuffer.Len()-r.checkpointBytes >= 64<<10 {
		snapshot := r.call
		snapshot.Content = r.contentBuffer.String()
		snapshot.Reasoning = r.reasoningBuffer.String()
		if r.truncated {
			snapshot.RecordingStatus = "partial"
		}
		_ = r.store.SaveCall(snapshot, nil)
		r.checkpointAt = time.Now()
		r.checkpointBytes = r.contentBuffer.Len() + r.reasoningBuffer.Len()
	}
}
func (c *TelemetryClient) begin(ctx context.Context, req agent.LLMRequest, started time.Time) (context.Context, *callRecord) {
	m := MetadataFromContext(ctx)
	store := c.store
	if store == nil {
		store = observability.FromContext(ctx)
	}
	r := &callRecord{store: store, call: observability.Call{CallID: uuid.NewString(), RequestID: m.RequestID, RequestIDs: m.RequestIDs, SessionID: m.SessionID, RunID: m.RunID, AgentID: m.AgentID, ProviderID: req.ProviderID, ModelID: req.Model, UsageType: m.UsageType, Status: "running", StartedAt: started, RecordingStatus: "complete", ToolCallID: m.ToolCallID}}
	if scope, ok := dispatch.ScopeFromContext(ctx); ok {
		r.call.DispatchID = scope.ParentID
		r.call.ParentDispatchID = m.ParentDispatchID
	}
	p := &observability.Prompt{RecordingStatus: "complete", Representation: "messages", Messages: []observability.Instruction{}}
	for _, msg := range req.Messages {
		if msg.Role == "system" || msg.Role == "developer" {
			p.Messages = append(p.Messages, observability.Instruction{Role: msg.Role, Content: msg.Content})
		}
	}
	if store != nil {
		_ = store.Start(m.SessionID, m.RequestID)
		if e := store.SaveCall(r.call, p); e != nil {
			r.call.RecordingStatus = "partial"
		}
	}
	r.observer = llm.NewCallObserver(func(text string) {
		if store != nil {
			snapshot := &observability.Prompt{RecordingStatus: "complete", Representation: "instructions", Messages: []observability.Instruction{}, Instructions: text}
			_ = store.SaveCall(r.call, snapshot)
		}
	})
	return context.WithValue(llm.WithCallObserver(ctx, r.observer), callRecordKey{}, r), r
}
