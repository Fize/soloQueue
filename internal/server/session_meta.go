package server

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// rememberRequestPrompt keeps the first prompt of a request for the session
// title generated when its first reply completes.
func (h *Hub) rememberRequestPrompt(requestID, prompt string) {
	if requestID == "" || prompt == "" {
		return
	}
	h.prompts.LoadOrStore(requestID, prompt)
}

func (h *Hub) forgetRequestPrompt(requestID string) { h.prompts.Delete(requestID) }

// observeSessionMeta maintains L2 session titles and plan status for every
// request origin and publishes the change into the session transcript.
func (h *Hub) observeSessionMeta(sessionID, requestID string, ev agent.AgentEvent) {
	if h == nil || h.mux == nil || h.mux.l2Store == nil || !strings.HasPrefix(sessionID, "l2:") {
		return
	}
	l2ID := strings.TrimPrefix(sessionID, "l2:")
	switch e := ev.(type) {
	case agent.DoneEvent:
		if h.mux.l2Store.GetName(l2ID) != "" {
			return
		}
		prompt, _ := h.prompts.Load(requestID)
		text, _ := prompt.(string)
		title := generateSessionTitle(text, e.Content)
		if title == "" {
			return
		}
		h.mux.l2Store.SetName(l2ID, title)
		h.publishSessionMeta(sessionID, transcript.Frame{Kind: transcript.KindSessionMeta, SessionName: title})
	case agent.ToolExecDoneEvent:
		if e.Err != nil {
			return
		}
		paths := planToolPaths(e.Name, e.Result)
		if len(paths) == 0 {
			return
		}
		entry := h.mux.l2Store.GetEntry(l2ID)
		if entry == nil || entry.Group == "" {
			return
		}
		prefix := filepath.Join(h.mux.workDir, "plan", entry.Group) + string(filepath.Separator)
		updated := false
		for _, p := range paths {
			if strings.HasPrefix(p, prefix) {
				h.mux.l2Store.UpdatePlanStatus(l2ID, p)
				updated = true
			}
		}
		if !updated {
			return
		}
		if refreshed := h.mux.l2Store.GetEntry(l2ID); refreshed != nil {
			h.publishSessionMeta(sessionID, transcript.Frame{Kind: transcript.KindSessionMeta, Plans: refreshed.Plans})
		}
	}
}

func (h *Hub) publishSessionMeta(sessionID string, frame transcript.Frame) {
	if h.mux == nil {
		return
	}
	h.mux.transcripts.Journal(sessionID, h.mux.sessionTimelineDir(sessionID)).Publish(frame)
}

// planToolPaths returns the files written by a successful file tool.
func planToolPaths(name, result string) []string {
	var paths []string
	switch name {
	case "Write", "Edit", "MultiEdit":
		var res struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(result), &res) == nil && res.Path != "" {
			paths = append(paths, res.Path)
		}
	case "MultiWrite":
		var res struct {
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		}
		if json.Unmarshal([]byte(result), &res) == nil {
			for _, f := range res.Files {
				if f.Path != "" {
					paths = append(paths, f.Path)
				}
			}
		}
	}
	return paths
}
