package session

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

// DefaultContextLimit resolves the default model context window limit from runtime settings.
func (b *Builder) DefaultContextLimit() int {
	if b != nil && b.RT != nil {
		dm := b.RT.ReadDefaultModel()
		if dm != nil && dm.ContextWindow > 0 {
			return dm.ContextWindow
		}
	}
	return 1048576 // fallback default
}

// ReadL2ContextUsage calculates context window usage for an L2 session without activating it.
// Per §5.4 of the repair plan, this helper is pure-read and creates no agent, supervisor,
// registry entry, timeline writer, or runtime watch.
func (b *Builder) ReadL2ContextUsage(ctx context.Context, id, group, workDir string) (used int, limit int, err error) {
	limit = b.DefaultContextLimit()
	if b == nil || b.RT == nil {
		return 0, limit, nil
	}

	// 1. Resolve leader template matching group
	var leaderTmpl *agent.AgentTemplate
	for i := range b.RT.AllTemplates {
		t := &b.RT.AllTemplates[i]
		if t.IsLeader && strings.EqualFold(t.Group, group) {
			leaderTmpl = t
			break
		}
	}

	if leaderTmpl != nil {
		modelID := leaderTmpl.ModelID
		if modelID != "" && b.Cfg != nil {
			for _, m := range b.Cfg.Get().Models {
				if m.ID == modelID && m.ContextWindow > 0 {
					limit = m.ContextWindow
					break
				}
			}
		}
	}

	// 2. Create ephemeral ContextWindow with no compactor side effects or writer
	cw := ctxwin.NewContextWindow(limit, 2000, 0, b.RT.Tokenizer)

	cw.SetReplayMode(true)

	// 3. Push system prompt if present
	if b.RT.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, b.RT.SystemPrompt)
	}

	// 4. Replay timeline segments if directory exists
	tlDir := filepath.Join(b.WorkDir, "logs", "timelines", "l2-"+id)
	segments, _, readErr := timeline.ReadTail(tlDir, "timeline.jsonl", 0, "")
	if readErr == nil && len(segments) > 0 {
		timeline.ReplayInto(cw, segments)
	}

	cw.SetReplayMode(false)

	used, limit, _ = cw.TokenUsage()
	return used, limit, nil
}
