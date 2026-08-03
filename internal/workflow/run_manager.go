package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

// NodeRunView and RunDetail are transport-safe snapshots. They deliberately do
// not expose the mutable engine RunState to HTTP handlers.
type NodeRunView struct {
	ID           string          `json:"id"`
	NodeID       string          `json:"node_id"`
	Attempt      int             `json:"attempt"`
	ActivationID string          `json:"activation_id"`
	State        NodeRunState    `json:"state"`
	Inputs       []NodeInputView `json:"inputs"`
	Result       *HandoffView    `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
	StartedAt    string          `json:"started_at"`
	FinishedAt   string          `json:"finished_at,omitempty"`
}

type NodeInputView struct {
	FromNode     string `json:"from_node"`
	Outcome      string `json:"outcome"`
	Content      string `json:"content"`
	ActivationID string `json:"activation_id"`
}

type HandoffView struct {
	Outcome string `json:"outcome"`
	Content string `json:"content"`
}
type TerminalOutputView struct {
	Node    string `json:"node"`
	Outcome string `json:"outcome"`
	Content string `json:"content"`
}

type RunSummary struct {
	ID             string    `json:"id"`
	WorkflowName   string    `json:"workflow_name"`
	Status         RunStatus `json:"status"`
	StartedAt      string    `json:"started_at"`
	FinishedAt     string    `json:"finished_at,omitempty"`
	Input          string    `json:"input"`
	NodeCount      int       `json:"node_count"`
	CompletedCount int       `json:"completed_count"`
	FailedCount    int       `json:"failed_count"`
}

type RunDetail struct {
	RunSummary
	NodeRuns        []NodeRunView        `json:"node_runs"`
	TerminalOutputs []TerminalOutputView `json:"terminal_outputs"`
	Edges           []WorkflowEdge       `json:"edges"`
}

// WorkflowEdge is the API representation of a validated transition.
type WorkflowEdge struct {
	FromNode      string `json:"from_node"`
	Outcome       string `json:"outcome"`
	ToNode        string `json:"to_node"`
	Loop          bool   `json:"loop"`
	MaxTraversals int    `json:"max_traversals"`
}

type RunManager struct {
	engine  *Engine
	db      *db.DB
	workDir string

	mu     sync.RWMutex
	active map[string]context.CancelFunc
	runs   map[string]RunDetail
}

func NewRunManager(engine *Engine, db *db.DB, workDir string) *RunManager {
	m := &RunManager{engine: engine, db: db, workDir: workDir, active: make(map[string]context.CancelFunc), runs: make(map[string]RunDetail)}
	m.reconcileInterrupted()
	return m
}

// A process restart cannot resume a running agent execution. Mark persisted
// in-flight snapshots terminal so the UI never polls an orphan forever.
func (m *RunManager) reconcileInterrupted() {
	if m.db == nil {
		return
	}
	rows, err := m.db.Query(`SELECT snapshot_json FROM workflow_runs WHERE status IN ('pending', 'running')`)
	if err != nil {
		return
	}
	var interrupted []RunDetail
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		var detail RunDetail
		if json.Unmarshal([]byte(raw), &detail) != nil {
			continue
		}
		detail.Status = RunFailed
		detail.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		for i := range detail.NodeRuns {
			if detail.NodeRuns[i].State == NodeRunning || detail.NodeRuns[i].State == NodeQueued {
				detail.NodeRuns[i].State = NodeCancelled
				detail.NodeRuns[i].FinishedAt = detail.FinishedAt
				detail.NodeRuns[i].Error = "workflow interrupted by server restart"
			}
		}
		interrupted = append(interrupted, detail)
	}
	_ = rows.Close()
	for _, detail := range interrupted {
		m.persist(detail)
	}
}

func (m *RunManager) Start(ctx context.Context, wf *ParsedWorkflow, input string) (string, error) {
	if m == nil || m.engine == nil {
		return "", fmt.Errorf("workflow: run manager not configured")
	}
	id := newRunID()
	runCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.active[id] = cancel
	m.mu.Unlock()
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.active, id)
			m.mu.Unlock()
			cancel()
		}()
		_, err := m.engine.RunWithOptions(runCtx, wf, input, m.workDir, RunOptions{ID: id, Observer: m.publish})
		if err != nil {
			m.publishFailure(id, wf.Name, input, err)
		}
	}()
	return id, nil
}

func (m *RunManager) Cancel(id string) bool {
	m.mu.RLock()
	cancel, ok := m.active[id]
	m.mu.RUnlock()
	if ok {
		cancel()
	}
	return ok
}

func (m *RunManager) Get(id string) (*RunDetail, error) {
	m.mu.RLock()
	if detail, ok := m.runs[id]; ok {
		copy := detail
		m.mu.RUnlock()
		return &copy, nil
	}
	m.mu.RUnlock()
	if m.db == nil {
		return nil, fmt.Errorf("workflow: run not found: %s", id)
	}
	var raw string
	if err := m.db.QueryRow(`SELECT snapshot_json FROM workflow_runs WHERE id = ?`, id).Scan(&raw); err != nil {
		return nil, fmt.Errorf("workflow: run not found: %s", id)
	}
	var detail RunDetail
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return nil, fmt.Errorf("workflow: decode run %s: %w", id, err)
	}
	return &detail, nil
}

func (m *RunManager) List(workflowName string) ([]RunSummary, error) {
	if m.db == nil {
		return []RunSummary{}, nil
	}
	rows, err := m.db.Query(`SELECT snapshot_json FROM workflow_runs WHERE workflow_name = ? ORDER BY started_at DESC LIMIT 100`, workflowName)
	if err != nil {
		return nil, fmt.Errorf("workflow: list runs: %w", err)
	}
	defer rows.Close()
	result := []RunSummary{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var detail RunDetail
		if err := json.Unmarshal([]byte(raw), &detail); err != nil {
			return nil, err
		}
		result = append(result, detail.RunSummary)
	}
	return result, rows.Err()
}

func (m *RunManager) publish(rs *RunState) {
	detail := snapshotRun(rs)
	m.mu.Lock()
	m.runs[detail.ID] = detail
	m.mu.Unlock()
	m.persist(detail)
}

func (m *RunManager) publishFailure(id, workflowName, input string, err error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	detail := RunDetail{RunSummary: RunSummary{ID: id, WorkflowName: workflowName, Status: RunFailed, StartedAt: now, FinishedAt: now, Input: input}}
	m.mu.Lock()
	m.runs[id] = detail
	m.mu.Unlock()
	m.persist(detail)
}

func (m *RunManager) persist(detail RunDetail) {
	if m.db == nil {
		return
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return
	}
	m.db.WMu.Lock()
	defer m.db.WMu.Unlock()
	_, _ = m.db.Exec(`INSERT INTO workflow_runs(id, workflow_name, status, started_at, finished_at, snapshot_json)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, finished_at=excluded.finished_at, snapshot_json=excluded.snapshot_json`,
		detail.ID, detail.WorkflowName, detail.Status, detail.StartedAt, detail.FinishedAt, string(raw))
}

func snapshotRun(rs *RunState) RunDetail {
	detail := RunDetail{RunSummary: RunSummary{ID: rs.ID, WorkflowName: rs.Workflow.Name, Status: rs.Status, StartedAt: rs.StartedAt.UTC().Format(time.RFC3339Nano), Input: rs.Input}}
	if !rs.FinishedAt.IsZero() {
		detail.FinishedAt = rs.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	for _, edge := range rs.Workflow.Edges {
		detail.Edges = append(detail.Edges, WorkflowEdge{FromNode: edge.FromNode, Outcome: edge.Outcome, ToNode: edge.ToNode, Loop: edge.Loop, MaxTraversals: edge.MaxTraversals})
	}
	for _, nr := range rs.NodeRuns {
		view := NodeRunView{ID: nr.ID, NodeID: nr.NodeID, Attempt: nr.Attempt, ActivationID: nr.ActivationID, State: nr.State}
		for _, input := range nr.Inputs {
			view.Inputs = append(view.Inputs, NodeInputView{FromNode: input.FromNode, Outcome: input.Outcome, Content: input.Content, ActivationID: input.ActivationID})
		}
		if nr.Result != nil {
			view.Result = &HandoffView{Outcome: nr.Result.Outcome, Content: nr.Result.Content}
		}
		if nr.Error != nil {
			view.Error = nr.Error.Error()
		}
		if !nr.StartedAt.IsZero() {
			view.StartedAt = nr.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		if !nr.FinishedAt.IsZero() {
			view.FinishedAt = nr.FinishedAt.UTC().Format(time.RFC3339Nano)
		}
		detail.NodeRuns = append(detail.NodeRuns, view)
		detail.NodeCount++
		if nr.State.IsTerminal() {
			detail.CompletedCount++
		}
		if nr.State == NodeFailed || nr.State == NodeTimedOut {
			detail.FailedCount++
		}
	}
	sort.Slice(detail.NodeRuns, func(i, j int) bool { return detail.NodeRuns[i].StartedAt < detail.NodeRuns[j].StartedAt })
	for _, output := range rs.TerminalOutput {
		detail.TerminalOutputs = append(detail.TerminalOutputs, TerminalOutputView{Node: output.Node, Outcome: output.Outcome, Content: output.Content})
	}
	return detail
}
