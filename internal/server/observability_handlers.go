package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func (m *Mux) registerObservabilityRoutes(r chi.Router) {
	r.Get("/api/observability/request", m.handleObservationRequest)
	r.Get("/api/observability/prompt", m.handleObservationPrompt)
	r.Get("/api/observability/dispatch", m.handleObservationDispatch)
}
func observationIdentity(r *http.Request) (string, string, bool) {
	s := r.URL.Query().Get("session_id")
	q := r.URL.Query().Get("request_id")
	_, e := session.ParseSessionID(s)
	return s, q, e == nil && len(q) > 0 && len(q) <= 256 && !strings.ContainsAny(q, "/\\\x00")
}
func (m *Mux) handleObservationRequest(w http.ResponseWriter, r *http.Request) {
	s, q, ok := observationIdentity(r)
	if !ok {
		m.writeJSON(w, 400, map[string]string{"error": "invalid session or request identity"})
		return
	}
	detail, e := observability.ForWorkDir(m.workDir).Request(s, q)
	if e != nil {
		m.writeJSON(w, 500, map[string]string{"error": "execution details unavailable"})
		return
	}
	// Completed metric rows are the fallback for older/expired detail files; never infer a prompt.
	if m.sharedDB != nil {
		metricRequest := q
		if detail.JoinedTo != "" {
			metricRequest = detail.JoinedTo
		}
		calls, err := m.sharedDB.ObservationCalls(r.Context(), s, metricRequest)
		if err != nil {
			detail.Errors = append(detail.Errors, "call metrics unavailable")
		} else {
			known := map[string]int{}
			for i, c := range detail.Calls {
				known[c.CallID] = i
			}
			for _, c := range calls {
				if detail.MetricsBounded && (c.StartedAt.Before(detail.MetricsStartedAt) || detail.MetricsFinishedAt != nil && c.StartedAt.After(*detail.MetricsFinishedAt)) {
					continue
				}
				if index, ok := known[c.CallID]; ok {
					existing := &detail.Calls[index]
					if existing.Status == "running" {
						existing.Status = c.Status
						existing.FinishedAt = &c.FinishedAt
						existing.DurationMS = c.DurationMS
						existing.PromptTokens = c.PromptTokens
						existing.CompletionTokens = c.CompletionTokens
						existing.TotalTokens = c.TotalTokens
						existing.AttemptCount = c.AttemptCount
						existing.AttemptsReported = c.AttemptsReported
						existing.RetryCount = c.RetryCount
						existing.RecordingStatus = "partial"
						if detail.RecordingStatus != "expired" {
							detail.RecordingStatus = "partial"
						}
					}
					continue
				}
				if detail.RecordingStatus == "complete" {
					detail.RecordingStatus = "partial"
					detail.Errors = append(detail.Errors, "some call details were not recorded")
				}

				detail.Calls = append(detail.Calls, observability.Call{CallID: c.CallID, SessionID: s, RequestID: q, RunID: c.RunID, AgentID: c.AgentID, ProviderID: c.ProviderID, ModelID: c.ModelID, UsageType: c.UsageType, Status: c.Status, StartedAt: c.StartedAt, FinishedAt: &c.FinishedAt, DurationMS: c.DurationMS, RetryCount: c.RetryCount, AttemptCount: c.AttemptCount, AttemptsReported: c.AttemptsReported, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens, TotalTokens: c.TotalTokens, RecordingStatus: detail.RecordingStatus, MetricsOnly: true})
			}
		}
	}
	observability.Summarize(&detail)
	cursor, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if cursor < 0 {
		cursor = 0
	}
	// Page both span collections through the same cursor without counting tools as LLM calls.
	all := append(append([]observability.Call{}, detail.Calls...), detail.Tools...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].StartedAt.Equal(all[j].StartedAt) {
			return all[i].CallID < all[j].CallID
		}
		return all[i].StartedAt.Before(all[j].StartedAt)
	})
	cursor = min(cursor, len(all))
	end := min(cursor+limit, len(all))
	if end < len(all) {
		detail.NextCursor = strconv.Itoa(end)
	}
	detail.Calls = []observability.Call{}
	detail.Tools = []observability.Call{}
	for _, span := range all[cursor:end] {
		if span.Kind == "tool" {
			detail.Tools = append(detail.Tools, span)
		} else {
			detail.Calls = append(detail.Calls, span)
		}
	}
	dispatches := m.findObservationDispatches(s, q)
	if detail.Generation != "" {
		filtered := dispatches[:0]
		for _, d := range dispatches {
			if !d.CreatedAt.Before(detail.StartedAt) {
				filtered = append(filtered, d)
			}
		}
		dispatches = filtered
	}
	for _, d := range dispatches {
		if d.DetailsTruncated && detail.RecordingStatus == "complete" {
			detail.RecordingStatus = "partial"
		}
	}
	result := struct {
		observability.Request
		Dispatches []observationDispatch `json:"dispatches"`
	}{detail, dispatches}
	m.writeJSON(w, 200, result)
}
func (m *Mux) handleObservationPrompt(w http.ResponseWriter, r *http.Request) {
	s, q, ok := observationIdentity(r)
	id := r.URL.Query().Get("call_id")
	if !ok || id == "" || len(id) > 100 || strings.ContainsAny(id, "/\\\x00") {
		m.writeJSON(w, 400, map[string]string{"error": "invalid call identity"})
		return
	}
	p, e := observability.ForWorkDir(m.workDir).CallPrompt(s, q, id)
	if e != nil {
		m.writeJSON(w, 500, map[string]string{"error": "prompt snapshot unavailable"})
		return
	}
	m.writeJSON(w, 200, p)
}

type observationDispatch struct {
	dispatch.Record
	Events     []dispatch.Event `json:"events"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func (m *Mux) dispatchOwner(s string) string {
	if s == "l1" && m.sessionMgr != nil {
		if sess := m.sessionMgr.Session(); sess != nil {
			return sess.TargetID
		}
	}
	if strings.HasPrefix(s, "l2:") {
		return "l2-" + strings.TrimPrefix(s, "l2:") + "-session"
	}
	if s == "l1" {
		return "l1-session"
	}
	return s
}
func (m *Mux) findObservationDispatches(s, q string) []observationDispatch {
	out := []observationDispatch{}
	paths, _ := filepath.Glob(filepath.Join(m.workDir, "logs", "timelines", "*", "delegations", "dlg_*", "meta.json"))
	owner := m.dispatchOwner(s)
	for _, p := range paths {
		st, e := os.Lstat(p)
		if e != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
			continue
		}
		if !observationSafePath(m.workDir, p) {
			continue
		}
		b, e := os.ReadFile(p)
		if e != nil {
			continue
		}
		var rec dispatch.Record
		if json.Unmarshal(b, &rec) != nil || rec.OwnerSessionID != owner || q != "" && rec.RequestID != q {
			continue
		}
		out = append(out, observationDispatch{Record: rec, Events: []dispatch.Event{}})
		if len(out) >= 100 {
			break
		}
	}
	return out
}
func observationSafePath(root, path string) bool {
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return false
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return false
	}
	rel, e := filepath.Rel(root, resolved)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && resolved == filepath.Clean(path)
}
func (m *Mux) handleObservationDispatch(w http.ResponseWriter, r *http.Request) {
	s := r.URL.Query().Get("session_id")
	if _, e := session.ParseSessionID(s); e != nil {
		m.writeJSON(w, 400, map[string]string{"error": "invalid session identity"})
		return
	}
	id := r.URL.Query().Get("dispatch_id")
	if !strings.HasPrefix(id, "dlg_") || strings.ContainsAny(id, "/\\\x00") || len(id) > 100 {
		m.writeJSON(w, 400, map[string]string{"error": "invalid dispatch identity"})
		return
	}
	var found *observationDispatch
	var authorizedDir string
	metaPaths, _ := filepath.Glob(filepath.Join(m.workDir, "logs", "timelines", "*", "delegations", id, "meta.json"))
	for _, path := range metaPaths {
		if !observationSafePath(m.workDir, path) {
			continue
		}
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record dispatch.Record
		if json.Unmarshal(body, &record) == nil && record.ID == id && record.OwnerSessionID == m.dispatchOwner(s) {
			authorizedDir = filepath.Dir(path)
			found = &observationDispatch{Record: record, Events: []dispatch.Event{}}
			break
		}
	}
	if found == nil {
		m.writeJSON(w, 404, map[string]string{"error": "dispatch not found in session"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := dispatch.ReadEventPageForRecord(authorizedDir, found.Record, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		m.writeJSON(w, 500, map[string]string{"error": "dispatch events unavailable: " + err.Error()})
		return
	}
	found.Events = page.Events
	found.NextCursor = page.NextCursor
	m.writeJSON(w, 200, found)
}
