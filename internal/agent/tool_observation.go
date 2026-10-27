package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	"github.com/xiaobaitu/soloqueue/internal/observability"
)

// Capture at the owning Agent, before watcher fan-out, so child tools retain their
// identity and capture does not depend on a WebSocket consumer staying connected.
func (a *Agent) observeTool(ctx context.Context, ev AgentEvent) {
	store := observability.FromContext(ctx)
	if store == nil {
		return
	}
	m := telemetryctx.FromContext(ctx)
	c := observability.Call{Kind: "tool", SessionID: m.SessionID, RequestID: m.RequestID, RequestIDs: m.RequestIDs, RunID: m.RunID, AgentID: a.InstanceID, RecordingStatus: "complete"}
	if scope, ok := dispatch.ScopeFromContext(ctx); ok {
		c.DispatchID = scope.ParentID
	}
	iter := 0
	now := time.Now().UTC()
	switch e := ev.(type) {
	case ToolExecStartEvent:
		c.ToolCallID = e.CallID
		c.ToolName = e.Name
		c.Arguments = e.Args
		c.StartedAt = now
		c.Status = "running"
		iter = e.Iter
	case ToolExecDoneEvent:
		c.ToolCallID = e.CallID
		c.ToolName = e.Name
		c.Content = e.Result
		c.FinishedAt = &now
		c.StartedAt = now.Add(-e.Duration)
		c.DurationMS = e.Duration.Milliseconds()
		c.Status = "success"
		iter = e.Iter
		if e.Err != nil {
			c.Status = "error"
			c.Error = e.Err.Error()
		}
	default:
		return
	}
	c.CallID = fmt.Sprintf("tool-%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%s", m.SessionID, m.RequestID, a.InstanceID, c.DispatchID, iter, c.ToolCallID))))
	// Bound optional payload capture independently of tool execution and model context.
	for _, value := range []*string{&c.Arguments, &c.Content, &c.Error} {
		if len(*value) > 8<<20 {
			*value = (*value)[:8<<20]
			c.RecordingStatus = "partial"
		}
	}
	if err := store.SaveCall(c, nil); err != nil {
		a.logError(ctx, logger.CatTool, "tool trace recording failed", err)
	}
}
