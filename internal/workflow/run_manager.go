package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/workflow/audit"
	workflowdelivery "github.com/xiaobaitu/soloqueue/internal/workflow/delivery"
	"github.com/xiaobaitu/soloqueue/internal/workflow/worktree"
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

type JoinBucketView struct {
	NodeID       string          `json:"node_id"`
	ActivationID string          `json:"activation_id"`
	Received     []NodeInputView `json:"received"`
	Expected     []string        `json:"expected"`
}

type LoopCounterView struct {
	EdgeID       string `json:"edge_id"`
	ActivationID string `json:"activation_id"`
	Count        int    `json:"count"`
}

type ConfirmationView struct {
	CallID         string   `json:"call_id"`
	NodeRunID      string   `json:"node_run_id,omitempty"`
	ToolName       string   `json:"tool_name,omitempty"`
	Prompt         string   `json:"prompt"`
	Options        []string `json:"options,omitempty"`
	AllowInSession bool     `json:"allow_in_session"`
	Status         string   `json:"status"`
	Choice         string   `json:"choice,omitempty"`
	RequestedAt    string   `json:"requested_at"`
	ResolvedAt     string   `json:"resolved_at,omitempty"`
}

type RunSummary struct {
	ID               string          `json:"id"`
	WorkflowName     string          `json:"workflow_name"`
	Status           RunStatus       `json:"status"`
	StartedAt        string          `json:"started_at"`
	FinishedAt       string          `json:"finished_at,omitempty"`
	Input            string          `json:"input"`
	NodeCount        int             `json:"node_count"`
	CompletedCount   int             `json:"completed_count"`
	FailedCount      int             `json:"failed_count"`
	Task             WorkflowTask    `json:"task"`
	Source           string          `json:"source,omitempty"`
	WorkflowVersion  string          `json:"workflow_version,omitempty"`
	WorkflowHash     string          `json:"workflow_hash,omitempty"`
	WorkflowYAML     string          `json:"workflow_yaml,omitempty"`
	RepositoryPath   string          `json:"repository_path,omitempty"`
	BaseRef          string          `json:"base_ref,omitempty"`
	BaseCommit       string          `json:"base_commit,omitempty"`
	BranchName       string          `json:"branch_name,omitempty"`
	WorktreePath     string          `json:"worktree_path,omitempty"`
	WorktreeState    string          `json:"worktree_state,omitempty"`
	ParentRunID      string          `json:"parent_run_id,omitempty"`
	RestartedFrom    string          `json:"restarted_from_run_id,omitempty"`
	SuccessorRunID   string          `json:"successor_run_id,omitempty"`
	PauseMode        string          `json:"pause_mode,omitempty"`
	ResumeAvailable  bool            `json:"resume_available"`
	RestartAvailable bool            `json:"restart_available"`
	CleanupAvailable bool            `json:"cleanup_available"`
	QualityStatus    string          `json:"quality_status,omitempty"`
	DeliveryStatus   string          `json:"delivery_status,omitempty"`
	Delivery         DeliveryRequest `json:"delivery,omitempty"`
	DeliveryResult   json.RawMessage `json:"delivery_result,omitempty"`
	AuditDir         string          `json:"audit_dir,omitempty"`
	AuditHeadHash    string          `json:"audit_head_hash,omitempty"`
	ErrorCode        string          `json:"error_code,omitempty"`
	ErrorMessage     string          `json:"error_message,omitempty"`
}

type RunDetail struct {
	RunSummary
	NodeRuns        []NodeRunView        `json:"node_runs"`
	TerminalOutputs []TerminalOutputView `json:"terminal_outputs"`
	Edges           []WorkflowEdge       `json:"edges"`
	ReadyQueue      []string             `json:"ready_queue,omitempty"`
	JoinBuckets     []JoinBucketView     `json:"join_buckets,omitempty"`
	LoopCounters    []LoopCounterView    `json:"loop_counters,omitempty"`
	Confirmations   []ConfirmationView   `json:"confirmations,omitempty"`
}

// WorkflowEdge is the API representation of a validated transition.
type WorkflowEdge struct {
	FromNode       string `json:"from_node"`
	Outcome        string `json:"outcome"`
	ToNode         string `json:"to_node"`
	Loop           bool   `json:"loop"`
	MaxTraversals  int    `json:"max_traversals"`
	TerminalStatus string `json:"terminal_status,omitempty"`
}

type RunManager struct {
	engine  *Engine
	db      *db.DB
	workDir string

	mu                 sync.RWMutex
	stateMu            sync.Mutex
	eventMu            sync.Mutex
	active             map[string]*runControl
	runs               map[string]RunDetail
	meta               map[string]*runMetadata
	confirms           map[string]pendingConfirmation
	persistedNodes     map[string][sha256.Size]byte
	persistedSnapshots map[string][sha256.Size]byte
	publishedSnapshots map[string][sha256.Size]byte
	wt                 *worktree.Manager
	auditDir           string
}

type runControl struct {
	cancel          context.CancelFunc
	pauseMode       string
	cancelRequested bool
}

type runMetadata struct {
	task               WorkflowTask
	workflowYAML       string
	workflowHash       string
	workflowVersion    string
	source             string
	worktree           worktree.Record
	parentRunID        string
	restartedFromRunID string
	audit              *audit.Log
}

type pendingConfirmation struct {
	runID   string
	resolve func(string) error
}

type RunEventView struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"run_id"`
	NodeRunID string          `json:"node_run_id,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	PrevHash  string          `json:"prev_hash,omitempty"`
	Hash      string          `json:"hash"`
	CreatedAt string          `json:"created_at"`
}

var workflowSecretPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|password|secret)(\s*[=:]\s*)([^\s,]+)`)

const maxRetainedCheckpoints = 64
const worktreeInspectTimeout = 30 * time.Second

func NewRunManager(engine *Engine, db *db.DB, workDir string) *RunManager {
	stateRoot := filepath.Join(os.TempDir(), "soloqueue")
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		stateRoot = filepath.Join(home, ".soloqueue")
	}
	return newRunManagerWithStateRoot(engine, db, workDir, stateRoot)
}

func newRunManagerWithStateRoot(engine *Engine, db *db.DB, workDir, stateRoot string) *RunManager {
	wtRoot := filepath.Join(stateRoot, "workflow-worktrees")
	wt, wtErr := worktree.NewManager(wtRoot)
	if wtErr != nil {
		fallbackRoot := filepath.Join(os.TempDir(), "soloqueue", "workflow-worktrees")
		wt, _ = worktree.NewManager(fallbackRoot)
		stateRoot = filepath.Dir(fallbackRoot)
	}
	auditDir := filepath.Join(stateRoot, "workflow-audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		auditDir = filepath.Join(os.TempDir(), "soloqueue", "workflow-audit")
		_ = os.MkdirAll(auditDir, 0o755)
	}
	m := &RunManager{engine: engine, db: db, workDir: workDir, active: make(map[string]*runControl), runs: make(map[string]RunDetail), meta: make(map[string]*runMetadata), confirms: make(map[string]pendingConfirmation), persistedNodes: make(map[string][sha256.Size]byte), persistedSnapshots: make(map[string][sha256.Size]byte), publishedSnapshots: make(map[string][sha256.Size]byte), wt: wt, auditDir: auditDir}
	m.reconcileInterrupted()
	return m
}

// A process restart cannot resume a running agent execution. Mark persisted
// in-flight snapshots terminal so the UI never polls an orphan forever.
func (m *RunManager) reconcileInterrupted() {
	if m.db == nil {
		return
	}
	rows, err := m.db.Query(`SELECT snapshot_json FROM workflow_runs WHERE status IN ('pending', 'preparing_worktree', 'running', 'pause_requested', 'resuming')`)
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
		detail.Status = RunInterrupted
		metadataAvailable := strings.TrimSpace(detail.WorkflowYAML) != "" && detail.WorktreePath != "" && detail.WorktreeState != "cleaned" && detail.Task.Validate() == nil
		detail.ResumeAvailable = metadataAvailable
		detail.RestartAvailable = metadataAvailable
		detail.CleanupAvailable = detail.WorktreePath != ""
		if !metadataAvailable {
			detail.ErrorCode = "workflow_legacy_metadata_missing"
			detail.ErrorMessage = "This run predates durable workflow metadata; only abandon is available."
		}
		detail.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		for i := range detail.NodeRuns {
			if detail.NodeRuns[i].State == NodeRunning || detail.NodeRuns[i].State == NodeQueued {
				detail.NodeRuns[i].State = NodeCancelled
				detail.NodeRuns[i].FinishedAt = detail.FinishedAt
				detail.NodeRuns[i].Error = "workflow interrupted by server restart; resume requires user action"
			}
		}
		interrupted = append(interrupted, detail)
	}
	_ = rows.Close()
	for _, detail := range interrupted {
		m.persist(detail)
		m.expireConfirmations(detail.ID, "interrupted")
		m.recordEvent(detail.ID, "run_interrupted", "", map[string]any{"reason": "server restart", "resume_available": detail.ResumeAvailable})
	}
}

// StartTask is the durable, isolated workflow entry point. It validates the
// structured task, creates a Git worktree before starting any agent, and
// persists the initial record synchronously so a crash cannot hide the run.
func (m *RunManager) StartTask(ctx context.Context, wf *ParsedWorkflow, workflowYAML []byte, task WorkflowTask, repository, baseRef, branch, source string) (string, error) {
	return m.startTask(ctx, wf, workflowYAML, task, repository, baseRef, branch, source, "", "")
}

func (m *RunManager) startTask(ctx context.Context, wf *ParsedWorkflow, workflowYAML []byte, task WorkflowTask, repository, baseRef, branch, source, parentRunID, restartedFrom string) (string, error) {
	if m == nil || m.engine == nil || m.wt == nil {
		return "", fmt.Errorf("workflow: run manager not configured")
	}
	task = task.Normalized()
	if err := task.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(repository) == "" {
		repository = m.workDir
	}
	id := newRunID()
	wt, err := m.wt.Prepare(ctx, id, repository, baseRef, branch)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(workflowYAML)
	workflowHash := fmt.Sprintf("%x", hash[:])
	metadata := &runMetadata{task: task, workflowYAML: string(workflowYAML), workflowHash: workflowHash, workflowVersion: wf.Version, source: source, worktree: wt, parentRunID: parentRunID, restartedFromRunID: restartedFrom}
	metadata.audit, err = audit.Open(m.auditDir, id)
	if err != nil {
		_ = m.wt.Remove(ctx, wt, true)
		return "", err
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	detail := RunDetail{RunSummary: RunSummary{ID: id, WorkflowName: wf.Name, Status: RunPreparing, StartedAt: started, Input: task.PromptInput(), Task: task, Source: source, RepositoryPath: wt.RepositoryPath, BaseRef: wt.BaseRef, BaseCommit: wt.BaseCommit, BranchName: wt.Branch, WorktreePath: wt.Path, WorktreeState: wt.State, ParentRunID: parentRunID, RestartedFrom: restartedFrom, ResumeAvailable: false, RestartAvailable: true, CleanupAvailable: true, AuditDir: m.auditDir}}
	m.mu.Lock()
	m.meta[id] = metadata
	m.runs[id] = detail
	m.mu.Unlock()
	m.persist(detail)
	m.recordEvent(id, "run_created", "", map[string]any{"workflow": wf.Name, "source": source, "worktree": wt})
	runCtx, cancel := context.WithCancel(ctx)
	control := &runControl{cancel: cancel}
	m.mu.Lock()
	m.active[id] = control
	m.mu.Unlock()
	go func() {
		defer func() {
			if m.releaseControl(id, control) {
				m.expireConfirmations(id, "unavailable")
				m.closeAudit(id)
			}
			cancel()
		}()
		runState, runErr := m.engine.RunWithOptions(runCtx, wf, task.PromptInput(), wt.Path, RunOptions{ID: id, Observer: m.publish, PauseRequested: func() (string, bool) { return m.pauseState(id) }, CancelRequested: func() bool { return m.cancelState(id) }, RecordConfirmation: func(req ConfirmationRequest) { m.recordConfirmation(id, req) }})
		if runErr == nil && runState != nil {
			m.runDelivery(runCtx, id, runState.Status)
		}
		if runErr != nil {
			m.publishFailure(id, wf.Name, task.PromptInput(), runErr)
		}
	}()
	return id, nil
}

func (m *RunManager) Cancel(id string) bool {
	m.mu.RLock()
	control, ok := m.active[id]
	m.mu.RUnlock()
	if ok {
		m.mu.Lock()
		control.cancelRequested = true
		m.mu.Unlock()
		control.cancel()
		m.recordEvent(id, "cancel_requested", "", map[string]any{})
	}
	return ok
}

// Pause requests a boundary pause. Graceful waits for the active node to
// finish; force cancels the active node and records a new attempt boundary.
func (m *RunManager) Pause(id, mode string) error {
	if mode == "" {
		mode = "graceful"
	}
	if mode != "graceful" && mode != "force" {
		return fmt.Errorf("workflow_pause_invalid: mode must be graceful or force")
	}
	m.stateMu.Lock()
	m.mu.Lock()
	control, active := m.active[id]
	detail, found := m.runs[id]
	if !active || !found {
		m.mu.Unlock()
		m.stateMu.Unlock()
		return fmt.Errorf("workflow_pause_conflict: run is not active")
	}
	switch detail.Status {
	case RunPending, RunPreparing, RunRunning, RunResuming:
	default:
		m.mu.Unlock()
		m.stateMu.Unlock()
		return fmt.Errorf("workflow_pause_conflict: run status is %s", detail.Status)
	}
	control.pauseMode = mode
	detail.Status = RunPauseAsked
	detail.PauseMode = mode
	detail.ResumeAvailable = true
	m.runs[id] = detail
	m.mu.Unlock()
	m.persist(detail)
	m.stateMu.Unlock()
	m.recordEvent(id, "pause_requested", "", map[string]any{"mode": mode})
	if mode == "force" {
		control.cancel()
	}
	return nil
}

// Resume resumes a paused/interrupted run only after an explicit user action.
// It does not run during RunManager construction or process startup.
func (m *RunManager) Resume(ctx context.Context, id string, allowDirty bool) error {
	detail, err := m.Get(id)
	if err != nil {
		return err
	}
	if detail.Status != RunPaused && detail.Status != RunInterrupted {
		return fmt.Errorf("workflow_resume_conflict: run status is %s", detail.Status)
	}
	runCtx, cancel := context.WithCancel(ctx)
	control := &runControl{cancel: cancel}
	if err := m.claimControl(id, control); err != nil {
		cancel()
		return err
	}
	releaseClaim := func() {
		m.releaseControl(id, control)
		cancel()
	}
	metadata, wf, err := m.loadRunMetadata(id, detail)
	if err != nil {
		releaseClaim()
		return err
	}
	if metadata.worktree.Path == "" {
		releaseClaim()
		return fmt.Errorf("workflow_resume_unavailable: worktree metadata is missing")
	}
	if m.wt != nil {
		head, state, _, inspectErr := m.wt.Inspect(ctx, metadata.worktree)
		if inspectErr != nil {
			releaseClaim()
			return fmt.Errorf("workflow_resume_worktree_lost: %w", inspectErr)
		}
		if (state == "dirty" || (metadata.worktree.BaseCommit != "" && head != metadata.worktree.BaseCommit)) && !allowDirty {
			releaseClaim()
			return fmt.Errorf("workflow_resume_worktree_changed: explicit allow_dirty is required")
		}
	}
	if metadata.audit == nil {
		metadata.audit, err = audit.Open(detail.AuditDir, id)
		if err != nil {
			releaseClaim()
			return err
		}
	}
	m.mu.Lock()
	m.meta[id] = metadata
	detail.Status = RunResuming
	detail.ResumeAvailable = false
	m.runs[id] = *detail
	m.mu.Unlock()
	m.persist(*detail)
	m.recordEvent(id, "resume_requested", "", map[string]any{"allow_dirty": allowDirty})
	go func() {
		defer func() {
			if m.releaseControl(id, control) {
				m.expireConfirmations(id, "unavailable")
				m.closeAudit(id)
			}
			cancel()
		}()
		resumeInput := buildResumeInput(detail)
		runState, runErr := m.engine.RunWithOptions(runCtx, wf, metadata.task.PromptInput(), metadata.worktree.Path, RunOptions{ID: id, Observer: m.publish, PauseRequested: func() (string, bool) { return m.pauseState(id) }, CancelRequested: func() bool { return m.cancelState(id) }, RecordConfirmation: func(req ConfirmationRequest) { m.recordConfirmation(id, req) }, Resume: resumeInput})
		if runErr == nil && runState != nil {
			m.runDelivery(runCtx, id, runState.Status)
		}
		if runErr != nil {
			m.publishFailure(id, wf.Name, metadata.task.PromptInput(), runErr)
		}
	}()
	return nil
}

func buildResumeInput(detail *RunDetail) *ResumeInput {
	if detail == nil {
		return nil
	}
	input := &ResumeInput{
		ReadyQueue:   append([]string(nil), detail.ReadyQueue...),
		JoinBuckets:  make(map[JoinKey]*JoinBucket, len(detail.JoinBuckets)),
		LoopCounters: make(map[LoopKey]int, len(detail.LoopCounters)),
	}
	ready := make(map[string]bool, len(input.ReadyQueue))
	for _, id := range input.ReadyQueue {
		ready[id] = true
	}
	for _, view := range detail.NodeRuns {
		nodeRun := &NodeRun{ID: view.ID, NodeID: view.NodeID, Attempt: view.Attempt, ActivationID: view.ActivationID, State: view.State}
		for _, item := range view.Inputs {
			nodeRun.Inputs = append(nodeRun.Inputs, NodeInput{FromNode: item.FromNode, Outcome: item.Outcome, Content: item.Content, ActivationID: item.ActivationID})
		}
		if view.Result != nil {
			nodeRun.Result = &HandoffData{Outcome: view.Result.Outcome, Content: view.Result.Content}
		}
		switch nodeRun.State {
		case NodeRunning, NodeCancelled, NodeTimedOut:
			nodeRun.Attempt++
			nodeRun.ID = newNodeRunID(nodeRun.NodeID, nodeRun.Attempt)
			nodeRun.State = NodeQueued
			input.ReadyQueue = append(input.ReadyQueue, nodeRun.ID)
		case NodeQueued:
			if !ready[nodeRun.ID] {
				input.ReadyQueue = append(input.ReadyQueue, nodeRun.ID)
			}
		}
		input.NodeRuns = append(input.NodeRuns, nodeRun)
	}
	for _, view := range detail.JoinBuckets {
		bucket := &JoinBucket{Received: make(map[string]NodeInput, len(view.Received)), Expected: make(map[string]bool, len(view.Expected))}
		for _, item := range view.Received {
			bucket.Received[item.FromNode] = NodeInput{FromNode: item.FromNode, Outcome: item.Outcome, Content: item.Content, ActivationID: item.ActivationID}
		}
		for _, source := range view.Expected {
			bucket.Expected[source] = true
		}
		input.JoinBuckets[JoinKey{NodeID: view.NodeID, ActivationID: view.ActivationID}] = bucket
	}
	for _, view := range detail.LoopCounters {
		input.LoopCounters[LoopKey{EdgeID: view.EdgeID, ActivationID: view.ActivationID}] = view.Count
	}
	for _, output := range detail.TerminalOutputs {
		input.TerminalOutput = append(input.TerminalOutput, TerminalOutput{Node: output.Node, Outcome: output.Outcome, Content: output.Content})
	}
	return input
}

// Restart creates a new run and worktree linked to the previous run. The old
// run remains immutable history and is never silently reused.
func (m *RunManager) Restart(ctx context.Context, id string) (string, error) {
	detail, err := m.Get(id)
	if err != nil {
		return "", err
	}
	metadata, wf, err := m.loadRunMetadata(id, detail)
	if err != nil {
		return "", err
	}
	newID, err := m.startTask(ctx, wf, []byte(metadata.workflowYAML), metadata.task, metadata.worktree.RepositoryPath, metadata.worktree.BaseRef, "", metadata.source, id, id)
	if err != nil {
		return "", err
	}
	_ = m.updateSuccessor(id, newID)
	m.recordEvent(id, "restart_requested", "", map[string]any{"successor_run_id": newID})
	return newID, nil
}

// Abandon marks an interrupted or paused run as intentionally abandoned.
// It does not remove the worktree or audit log; cleanup is a separate action.
func (m *RunManager) Abandon(id string) error {
	m.mu.RLock()
	_, active := m.active[id]
	m.mu.RUnlock()
	if active {
		return fmt.Errorf("workflow_abandon_conflict: run is active")
	}
	detail, err := m.Get(id)
	if err != nil {
		return err
	}
	if detail.Status != RunInterrupted && detail.Status != RunPaused && detail.Status != RunFailed && detail.Status != RunCancelled {
		return fmt.Errorf("workflow_abandon_conflict: run status is %s", detail.Status)
	}
	detail.Status = RunAbandoned
	detail.ResumeAvailable = false
	m.storeDetail(*detail)
	m.recordEvent(id, "run_abandoned", "", map[string]any{})
	return nil
}

// CleanupWorktree removes only the explicitly selected run worktree. Audit
// history and the database record remain available after cleanup.
func (m *RunManager) CleanupWorktree(ctx context.Context, id string, force bool) error {
	m.mu.RLock()
	_, active := m.active[id]
	m.mu.RUnlock()
	if active {
		return fmt.Errorf("workflow_cleanup_conflict: run is active")
	}
	detail, err := m.Get(id)
	if err != nil {
		return err
	}
	metadata, _, err := m.loadRunMetadata(id, detail)
	if err != nil {
		return err
	}
	if metadata.worktree.Path == "" || metadata.worktree.RepositoryPath == "" {
		return fmt.Errorf("workflow_cleanup_unavailable: worktree metadata is missing")
	}
	if err := m.wt.Remove(ctx, metadata.worktree, force); err != nil {
		return err
	}
	detail.WorktreeState = "cleaned"
	detail.ResumeAvailable = false
	detail.CleanupAvailable = false
	m.storeDetail(*detail)
	if m.db != nil {
		m.db.WMu.Lock()
		_, _ = m.db.Exec(`UPDATE workflow_worktrees SET state = 'cleaned', cleaned_at = ? WHERE workflow_run_id = ?`, time.Now().UTC().Format(time.RFC3339Nano), id)
		m.db.WMu.Unlock()
	}
	m.recordEvent(id, "worktree_cleaned", "", map[string]any{"force": force})
	return nil
}

func (m *RunManager) pauseState(id string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	control, ok := m.active[id]
	if !ok || control.pauseMode == "" {
		return "", false
	}
	return control.pauseMode, true
}

func (m *RunManager) cancelState(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	control, ok := m.active[id]
	return ok && control.cancelRequested
}

func (m *RunManager) releaseControl(id string, control *runControl) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[id] != control {
		return false
	}
	delete(m.active, id)
	return true
}

func (m *RunManager) claimControl(id string, control *runControl) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.active[id]; exists {
		return fmt.Errorf("workflow_resume_conflict: run is already active")
	}
	m.active[id] = control
	return nil
}

func (m *RunManager) storeDetail(detail RunDetail) {
	m.mu.Lock()
	m.runs[detail.ID] = detail
	m.mu.Unlock()
	m.persist(detail)
}

func (m *RunManager) closeAudit(id string) {
	m.mu.Lock()
	metadata := m.meta[id]
	if metadata != nil && metadata.audit != nil {
		_ = metadata.audit.Close()
		metadata.audit = nil
	}
	m.mu.Unlock()
}

func (m *RunManager) loadRunMetadata(id string, detail *RunDetail) (*runMetadata, *ParsedWorkflow, error) {
	m.mu.RLock()
	metadata := m.meta[id]
	m.mu.RUnlock()
	if metadata != nil && strings.TrimSpace(metadata.workflowYAML) == "" {
		metadata = nil
	}
	if metadata == nil && m.db != nil {
		var workflowYAML, taskJSON, source, repo, baseRef, baseCommit, branch, path, auditDir, hash, version, parentID, restarted string
		err := m.db.QueryRow(`SELECT workflow_yaml, task_json, source, repository_path, base_ref, base_commit, branch_name, worktree_path, audit_dir, workflow_hash, workflow_version, parent_run_id, restarted_from_run_id FROM workflow_runs WHERE id = ?`, id).Scan(&workflowYAML, &taskJSON, &source, &repo, &baseRef, &baseCommit, &branch, &path, &auditDir, &hash, &version, &parentID, &restarted)
		if err != nil {
			return nil, nil, fmt.Errorf("workflow_resume_metadata: %w", err)
		}
		var task WorkflowTask
		if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
			return nil, nil, fmt.Errorf("workflow_resume_task: %w", err)
		}
		metadata = &runMetadata{task: task, workflowYAML: workflowYAML, workflowHash: hash, workflowVersion: version, source: source, parentRunID: parentID, restartedFromRunID: restarted, worktree: worktree.Record{RunID: id, RepositoryPath: repo, BaseRef: baseRef, BaseCommit: baseCommit, Branch: branch, Path: path, State: detail.WorktreeState}}
		if detail.AuditDir == "" {
			detail.AuditDir = auditDir
		}
	}
	if metadata == nil || strings.TrimSpace(metadata.workflowYAML) == "" {
		return nil, nil, fmt.Errorf("workflow_resume_unavailable: workflow definition is missing")
	}
	wf, err := ParseWorkflow([]byte(metadata.workflowYAML))
	if err != nil {
		return nil, nil, fmt.Errorf("workflow_resume_definition: %w", err)
	}
	return metadata, wf, nil
}

func (m *RunManager) updateSuccessor(id, successor string) error {
	m.mu.Lock()
	if detail, ok := m.runs[id]; ok {
		detail.SuccessorRunID = successor
		m.runs[id] = detail
	}
	m.mu.Unlock()
	if m.db == nil {
		return nil
	}
	m.db.WMu.Lock()
	defer m.db.WMu.Unlock()
	_, err := m.db.Exec(`UPDATE workflow_runs SET successor_run_id = ?, updated_at = ? WHERE id = ?`, successor, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (m *RunManager) Get(id string) (*RunDetail, error) {
	m.mu.RLock()
	if detail, ok := m.runs[id]; ok {
		copy := detail
		m.mu.RUnlock()
		m.attachConfirmations(&copy)
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
	m.attachConfirmations(&detail)
	return &detail, nil
}

func (m *RunManager) attachConfirmations(detail *RunDetail) {
	if m.db == nil || detail == nil {
		return
	}
	rows, err := m.db.Query(`SELECT call_id, node_run_id, tool_name, prompt_redacted, options_json, allow_in_session, status, choice, requested_at, resolved_at FROM workflow_confirmations WHERE workflow_run_id = ? ORDER BY requested_at`, detail.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	detail.Confirmations = nil
	for rows.Next() {
		var view ConfirmationView
		var optionsJSON string
		if rows.Scan(&view.CallID, &view.NodeRunID, &view.ToolName, &view.Prompt, &optionsJSON, &view.AllowInSession, &view.Status, &view.Choice, &view.RequestedAt, &view.ResolvedAt) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(optionsJSON), &view.Options)
		detail.Confirmations = append(detail.Confirmations, view)
	}
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
		// Definitions remain available from Get, but never inflate the summary list.
		detail.WorkflowYAML = ""
		result = append(result, detail.RunSummary)
	}
	return result, rows.Err()
}

func (m *RunManager) publish(rs *RunState) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	detail := snapshotRun(rs)
	if rs.Status == RunCompleted || rs.Status == RunFailed || rs.Status == RunBlocked || rs.Status == RunPaused || rs.Status == RunCancelled {
		m.refreshWorktreeState(rs.ID)
	}
	m.applyMetadata(&detail)
	m.mu.Lock()
	if previous, ok := m.runs[detail.ID]; ok && previous.StartedAt != "" {
		detail.StartedAt = previous.StartedAt
	}
	fingerprint := runDetailFingerprint(detail)
	if previous, ok := m.publishedSnapshots[detail.ID]; ok && previous == fingerprint {
		m.mu.Unlock()
		return
	}
	m.runs[detail.ID] = detail
	m.mu.Unlock()
	m.persist(detail)
	m.mu.Lock()
	if m.publishedSnapshots == nil {
		m.publishedSnapshots = make(map[string][sha256.Size]byte)
	}
	m.publishedSnapshots[detail.ID] = fingerprint
	m.mu.Unlock()
	m.recordEvent(detail.ID, "state_snapshot", "", map[string]any{"status": detail.Status, "node_count": detail.NodeCount, "completed_count": detail.CompletedCount, "failed_count": detail.FailedCount})
}

func runDetailFingerprint(detail RunDetail) [sha256.Size]byte {
	payload := struct {
		Status          RunStatus            `json:"status"`
		FinishedAt      string               `json:"finished_at"`
		PauseMode       string               `json:"pause_mode"`
		WorktreeState   string               `json:"worktree_state"`
		NodeRuns        []NodeRunView        `json:"node_runs"`
		TerminalOutputs []TerminalOutputView `json:"terminal_outputs"`
		ReadyQueue      []string             `json:"ready_queue"`
		JoinBuckets     []JoinBucketView     `json:"join_buckets"`
		LoopCounters    []LoopCounterView    `json:"loop_counters"`
	}{detail.Status, detail.FinishedAt, detail.PauseMode, detail.WorktreeState, detail.NodeRuns, detail.TerminalOutputs, detail.ReadyQueue, detail.JoinBuckets, detail.LoopCounters}
	raw, _ := json.Marshal(payload)
	return sha256.Sum256(raw)
}

func (m *RunManager) refreshWorktreeState(id string) {
	m.mu.RLock()
	metadata := m.meta[id]
	m.mu.RUnlock()
	if metadata == nil || metadata.worktree.Path == "" || m.wt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeInspectTimeout)
	defer cancel()
	_, state, _, err := m.wt.Inspect(ctx, metadata.worktree)
	if err == nil {
		m.mu.Lock()
		if current := m.meta[id]; current != nil {
			current.worktree.State = state
		}
		m.mu.Unlock()
	}
}

func (m *RunManager) publishFailure(id, workflowName, input string, err error) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	detail := RunDetail{RunSummary: RunSummary{ID: id, WorkflowName: workflowName, Status: RunFailed, StartedAt: now, FinishedAt: now, Input: input, ErrorCode: "workflow_execution_failed", ErrorMessage: err.Error()}}
	if existing, getErr := m.Get(id); getErr == nil {
		detail = *existing
		detail.Status = RunFailed
		detail.FinishedAt = now
		detail.ErrorCode = "workflow_execution_failed"
		detail.ErrorMessage = err.Error()
	}
	m.applyMetadata(&detail)
	m.mu.Lock()
	m.runs[id] = detail
	m.mu.Unlock()
	m.persist(detail)
	m.recordEvent(id, "run_failed", "", map[string]any{"error": err.Error()})
}

func (m *RunManager) persist(detail RunDetail) {
	if m.db == nil {
		return
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return
	}
	taskJSON, _ := detail.Task.JSON()
	deliveryJSON := detail.DeliveryResult
	if len(deliveryJSON) == 0 {
		deliveryJSON, _ = json.Marshal(detail.Delivery)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	snapshotFingerprint := sha256.Sum256(raw)
	m.db.WMu.Lock()
	defer m.db.WMu.Unlock()
	if previous, ok := m.persistedSnapshots[detail.ID]; ok && previous == snapshotFingerprint {
		return
	}
	tx, err := m.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	_, _ = tx.Exec(`INSERT INTO workflow_runs(
		id, workflow_name, status, started_at, finished_at, snapshot_json,
		workflow_version, workflow_hash, workflow_yaml, task_json, work_dir, source,
		repository_path, base_ref, base_commit, branch_name, worktree_path, worktree_state,
		parent_run_id, restarted_from_run_id, successor_run_id, pause_mode, resume_available,
		quality_status, delivery_status, delivery_json, error_code, error_message, audit_dir,
		audit_head_hash, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		status=excluded.status, finished_at=excluded.finished_at, snapshot_json=excluded.snapshot_json,
		workflow_version=excluded.workflow_version, workflow_hash=excluded.workflow_hash, workflow_yaml=excluded.workflow_yaml,
		task_json=excluded.task_json, work_dir=excluded.work_dir, source=excluded.source,
		repository_path=excluded.repository_path, base_ref=excluded.base_ref, base_commit=excluded.base_commit,
		branch_name=excluded.branch_name, worktree_path=excluded.worktree_path, worktree_state=excluded.worktree_state,
		parent_run_id=excluded.parent_run_id, restarted_from_run_id=excluded.restarted_from_run_id,
		successor_run_id=excluded.successor_run_id, pause_mode=excluded.pause_mode, resume_available=excluded.resume_available,
		quality_status=excluded.quality_status, delivery_status=excluded.delivery_status, delivery_json=excluded.delivery_json,
		error_code=excluded.error_code, error_message=excluded.error_message, audit_dir=excluded.audit_dir,
		audit_head_hash=excluded.audit_head_hash, updated_at=excluded.updated_at`,
		detail.ID, detail.WorkflowName, detail.Status, detail.StartedAt, detail.FinishedAt, string(raw),
		detail.WorkflowVersion, detail.WorkflowHash, detail.WorkflowYAML, taskJSON, detail.RepositoryPath, detail.Source,
		detail.RepositoryPath, detail.BaseRef, detail.BaseCommit, detail.BranchName, detail.WorktreePath, detail.WorktreeState,
		detail.ParentRunID, detail.RestartedFrom, detail.SuccessorRunID, detail.PauseMode, detail.ResumeAvailable,
		detail.QualityStatus, detail.DeliveryStatus, string(deliveryJSON), detail.ErrorCode, detail.ErrorMessage,
		detail.AuditDir, detail.AuditHeadHash, detail.StartedAt, now)
	persisted := make(map[string][sha256.Size]byte)
	for _, node := range detail.NodeRuns {
		nodeRaw, _ := json.Marshal(node)
		fingerprint := sha256.Sum256(nodeRaw)
		if previous, ok := m.persistedNodes[node.ID]; ok && previous == fingerprint {
			continue
		}
		inputs, _ := json.Marshal(node.Inputs)
		outcome, content := "", ""
		errorCode := ""
		if node.Result != nil {
			outcome, content = node.Result.Outcome, node.Result.Content
		}
		if node.Error != "" {
			errorCode = "node_execution_failed"
		}
		_, _ = tx.Exec(`INSERT INTO workflow_node_runs(id, workflow_run_id, node_id, attempt, activation_id, state, inputs_json, outcome, output_content, error_message, started_at, finished_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET state=excluded.state, inputs_json=excluded.inputs_json, outcome=excluded.outcome,
			output_content=excluded.output_content, error_message=excluded.error_message, started_at=excluded.started_at,
			finished_at=excluded.finished_at, updated_at=excluded.updated_at`,
			node.ID, detail.ID, node.NodeID, node.Attempt, node.ActivationID, node.State, string(inputs), outcome, content, node.Error, node.StartedAt, node.FinishedAt, now)
		if errorCode != "" {
			_, _ = tx.Exec(`UPDATE workflow_node_runs SET error_code = ? WHERE id = ?`, errorCode, node.ID)
		}
		persisted[node.ID] = fingerprint
	}
	// Checkpoints capture changed scheduler boundaries only. Recovery needs the
	// latest state, while long-term traceability remains in audit events.
	readyJSON, _ := json.Marshal(detail.ReadyQueue)
	joinJSON, _ := json.Marshal(detail.JoinBuckets)
	loopJSON, _ := json.Marshal(detail.LoopCounters)
	var seq int
	_ = tx.QueryRow(`SELECT COALESCE(MAX(sequence), 0) FROM workflow_run_checkpoints WHERE workflow_run_id = ?`, detail.ID).Scan(&seq)
	seq++
	_, _ = tx.Exec(`INSERT INTO workflow_run_checkpoints(workflow_run_id, sequence, engine_version, scheduler_json, ready_queue_json, join_buckets_json, loop_counters_json, completed_node_runs_json, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, detail.ID, seq, "workflow-v2", fmt.Sprintf(`{"status":%q}`, detail.Status), string(readyJSON), string(joinJSON), string(loopJSON), string(raw), now)
	_, _ = tx.Exec(`UPDATE workflow_runs SET checkpoint_sequence = ?, updated_at = ? WHERE id = ?`, seq, now, detail.ID)
	if seq > maxRetainedCheckpoints {
		_, _ = tx.Exec(`DELETE FROM workflow_run_checkpoints WHERE workflow_run_id = ? AND sequence <= ?`, detail.ID, seq-maxRetainedCheckpoints)
	}
	if detail.WorktreePath != "" && detail.RepositoryPath != "" {
		_, _ = tx.Exec(`INSERT INTO workflow_worktrees(id, workflow_run_id, kind, repository_path, path, branch, base_commit, state, created_at)
			VALUES(?, ?, 'main', ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET repository_path=excluded.repository_path, path=excluded.path,
			branch=excluded.branch, base_commit=excluded.base_commit, state=excluded.state`,
			detail.ID, detail.ID, detail.RepositoryPath, detail.WorktreePath, detail.BranchName, detail.BaseCommit, detail.WorktreeState, now)
	}
	if tx.Commit() == nil {
		if m.persistedSnapshots == nil {
			m.persistedSnapshots = make(map[string][sha256.Size]byte)
		}
		m.persistedSnapshots[detail.ID] = snapshotFingerprint
		if m.persistedNodes == nil {
			m.persistedNodes = make(map[string][sha256.Size]byte)
		}
		for id, fingerprint := range persisted {
			m.persistedNodes[id] = fingerprint
		}
	}
}

func (m *RunManager) applyMetadata(detail *RunDetail) {
	if detail == nil {
		return
	}
	m.mu.RLock()
	metadata := m.meta[detail.ID]
	m.mu.RUnlock()
	if metadata == nil {
		return
	}
	detail.Task = metadata.task
	detail.Delivery = metadata.task.Delivery
	detail.Source = metadata.source
	detail.WorkflowVersion = metadata.workflowVersion
	detail.WorkflowHash = metadata.workflowHash
	detail.WorkflowYAML = metadata.workflowYAML
	detail.RepositoryPath = metadata.worktree.RepositoryPath
	detail.BaseRef = metadata.worktree.BaseRef
	detail.BaseCommit = metadata.worktree.BaseCommit
	detail.BranchName = metadata.worktree.Branch
	detail.WorktreePath = metadata.worktree.Path
	detail.WorktreeState = metadata.worktree.State
	detail.ParentRunID = metadata.parentRunID
	detail.RestartedFrom = metadata.restartedFromRunID
	detail.AuditDir = m.auditDir
	detail.ResumeAvailable = (detail.Status == RunPaused || detail.Status == RunInterrupted) && metadata.worktree.State != "cleaned"
	detail.RestartAvailable = true
	detail.CleanupAvailable = metadata.worktree.Path != "" && metadata.worktree.State != "cleaned"
	if metadata.audit != nil {
		detail.AuditHeadHash = metadata.audit.Head()
	}
}

func (m *RunManager) recordEvent(id, eventType, nodeRunID string, payload any) {
	if m == nil {
		return
	}
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	m.mu.Lock()
	metadata := m.meta[id]
	if metadata == nil {
		metadata = &runMetadata{source: "recovery", worktree: worktree.Record{RunID: id}}
		if m.db != nil {
			var auditDir string
			_ = m.db.QueryRow(`SELECT audit_dir FROM workflow_runs WHERE id = ?`, id).Scan(&auditDir)
			if auditDir != "" {
				if opened, openErr := audit.Open(auditDir, id); openErr == nil {
					metadata.audit = opened
				}
			}
		}
		m.meta[id] = metadata
	}
	if metadata != nil && metadata.audit == nil {
		if opened, err := audit.Open(m.auditDir, id); err == nil {
			metadata.audit = opened
		}
	}
	var entry audit.Entry
	var err error
	if metadata != nil && metadata.audit != nil {
		entry, err = metadata.audit.Append(id, nodeRunID, eventType, payload)
	}
	if entry.Hash != "" {
		if detail, ok := m.runs[id]; ok {
			detail.AuditHeadHash = entry.Hash
			detail.AuditDir = m.auditDir
			m.runs[id] = detail
		}
	}
	m.mu.Unlock()
	if err != nil || m.db == nil || entry.Hash == "" {
		return
	}
	raw, _ := json.Marshal(payload)
	raw = []byte(workflowSecretPattern.ReplaceAllString(string(raw), `$1$2[REDACTED]`))
	m.db.WMu.Lock()
	defer m.db.WMu.Unlock()
	_, _ = m.db.Exec(`INSERT INTO workflow_run_events(workflow_run_id, node_run_id, event_type, payload_json, prev_hash, event_hash, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, id, nodeRunID, eventType, string(raw), entry.PrevHash, entry.Hash, entry.CreatedAt)
	_, _ = m.db.Exec(`UPDATE workflow_runs SET audit_head_hash = ?, audit_dir = ?, updated_at = ? WHERE id = ?`, entry.Hash, m.auditDir, entry.CreatedAt, id)
}

func (m *RunManager) recordConfirmation(id string, request ConfirmationRequest) {
	if request.CallID == "" {
		return
	}
	if request.Resolve != nil {
		m.mu.Lock()
		if m.confirms == nil {
			m.confirms = make(map[string]pendingConfirmation)
		}
		m.confirms[request.CallID] = pendingConfirmation{runID: id, resolve: request.Resolve}
		m.mu.Unlock()
	}
	if m.db == nil {
		return
	}
	prompt := workflowSecretPattern.ReplaceAllString(request.PromptRedacted, `$1$2[REDACTED]`)
	if len(prompt) > 2000 {
		prompt = prompt[:2000]
	}
	optionsJSON, _ := json.Marshal(request.Options)
	requestedAt := time.Now().UTC().Format(time.RFC3339Nano)
	m.db.WMu.Lock()
	_, _ = m.db.Exec(`INSERT INTO workflow_confirmations(call_id, workflow_run_id, node_run_id, tool_name, prompt_redacted, options_json, allow_in_session, status, requested_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, 'pending', ?)
		ON CONFLICT(call_id) DO UPDATE SET prompt_redacted=excluded.prompt_redacted, options_json=excluded.options_json, allow_in_session=excluded.allow_in_session, status='pending', requested_at=excluded.requested_at`, request.CallID, id, request.NodeRunID, request.ToolName, prompt, string(optionsJSON), request.AllowInSession, requestedAt)
	m.db.WMu.Unlock()
	m.recordEvent(id, "confirmation_requested", request.NodeRunID, map[string]any{"call_id": request.CallID, "tool_name": request.ToolName, "prompt": prompt})
}

func (m *RunManager) expireConfirmations(runID, status string) {
	m.mu.Lock()
	for callID, pending := range m.confirms {
		if pending.runID == runID {
			delete(m.confirms, callID)
		}
	}
	m.mu.Unlock()
	if m.db == nil {
		return
	}
	resolvedAt := time.Now().UTC().Format(time.RFC3339Nano)
	m.db.WMu.Lock()
	_, _ = m.db.Exec(`UPDATE workflow_confirmations SET status = ?, resolved_at = ? WHERE workflow_run_id = ? AND status = 'pending'`, status, resolvedAt, runID)
	m.db.WMu.Unlock()
}

func (m *RunManager) ResolveConfirmation(runID, callID, choice string) error {
	m.mu.RLock()
	pending, ok := m.confirms[callID]
	m.mu.RUnlock()
	if !ok || pending.runID != runID || pending.resolve == nil {
		return fmt.Errorf("workflow_confirmation_unavailable: confirmation is not pending for this run")
	}
	if err := pending.resolve(choice); err != nil {
		return fmt.Errorf("workflow_confirmation_resolve: %w", err)
	}
	m.mu.Lock()
	if current, exists := m.confirms[callID]; exists && current.runID == runID {
		delete(m.confirms, callID)
	}
	m.mu.Unlock()
	if m.db != nil {
		resolvedAt := time.Now().UTC().Format(time.RFC3339Nano)
		m.db.WMu.Lock()
		_, _ = m.db.Exec(`UPDATE workflow_confirmations SET status = 'resolved', choice = ?, resolved_at = ? WHERE call_id = ? AND workflow_run_id = ?`, choice, resolvedAt, callID, runID)
		m.db.WMu.Unlock()
	}
	m.recordEvent(runID, "confirmation_resolved", "", map[string]any{"call_id": callID, "choice": choice})
	return nil
}

func (m *RunManager) runDelivery(ctx context.Context, id string, status RunStatus) {
	m.mu.RLock()
	metadata := m.meta[id]
	m.mu.RUnlock()
	if metadata == nil || status != RunCompleted {
		return
	}
	request := workflowdelivery.Request{}
	if metadata.task.Delivery.Commit != nil {
		request.Commit = &workflowdelivery.CommitRequest{Enabled: metadata.task.Delivery.Commit.Enabled, Message: metadata.task.Delivery.Commit.Message}
	}
	if metadata.task.Delivery.Push != nil {
		request.Push = &workflowdelivery.PushRequest{Enabled: metadata.task.Delivery.Push.Enabled, Remote: metadata.task.Delivery.Push.Remote, Branch: metadata.task.Delivery.Push.Branch}
	}
	if metadata.task.Delivery.PullRequest != nil {
		request.PullRequest = &workflowdelivery.PullRequestRequest{Enabled: metadata.task.Delivery.PullRequest.Enabled, Title: metadata.task.Delivery.PullRequest.Title, Body: metadata.task.Delivery.PullRequest.Body, Draft: metadata.task.Delivery.PullRequest.Draft}
	}
	deliveryCtx, cancel := deliveryContext(ctx)
	defer cancel()
	result := workflowdelivery.Execute(deliveryCtx, metadata.worktree, request)
	m.refreshWorktreeState(id)
	detail, err := m.Get(id)
	if err != nil {
		return
	}
	detail.WorktreeState = metadata.worktree.State
	detail.DeliveryStatus = result.Status
	detail.DeliveryResult, _ = json.Marshal(result)
	m.storeDetail(*detail)
	m.recordEvent(id, "delivery_finished", "", result)
}

func deliveryContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, workflowdelivery.DefaultTimeout)
}

func (m *RunManager) Events(runID string, after int64, limit int) ([]RunEventView, error) {
	if m.db == nil {
		return []RunEventView{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := m.db.Query(`SELECT id, workflow_run_id, node_run_id, event_type, payload_json, prev_hash, event_hash, created_at FROM workflow_run_events WHERE workflow_run_id = ? AND id > ? ORDER BY id ASC LIMIT ?`, runID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("workflow: list events: %w", err)
	}
	defer rows.Close()
	result := []RunEventView{}
	for rows.Next() {
		var event RunEventView
		var payload string
		if err := rows.Scan(&event.ID, &event.RunID, &event.NodeRunID, &event.Type, &payload, &event.PrevHash, &event.Hash, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		result = append(result, event)
	}
	return result, rows.Err()
}

func snapshotRun(rs *RunState) RunDetail {
	detail := RunDetail{RunSummary: RunSummary{ID: rs.ID, WorkflowName: rs.Workflow.Name, Status: rs.Status, StartedAt: rs.StartedAt.UTC().Format(time.RFC3339Nano), Input: rs.Input}}
	if !rs.FinishedAt.IsZero() {
		detail.FinishedAt = rs.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	for _, edge := range rs.Workflow.Edges {
		detail.Edges = append(detail.Edges, WorkflowEdge{FromNode: edge.FromNode, Outcome: edge.Outcome, ToNode: edge.ToNode, Loop: edge.Loop, MaxTraversals: edge.MaxTraversals, TerminalStatus: edge.TerminalStatus})
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
	detail.ReadyQueue = append(detail.ReadyQueue, rs.ReadyQueue...)
	for key, bucket := range rs.JoinBuckets {
		view := JoinBucketView{NodeID: key.NodeID, ActivationID: key.ActivationID}
		for _, input := range bucket.Received {
			view.Received = append(view.Received, NodeInputView{FromNode: input.FromNode, Outcome: input.Outcome, Content: input.Content, ActivationID: input.ActivationID})
		}
		for source := range bucket.Expected {
			view.Expected = append(view.Expected, source)
		}
		sort.Slice(view.Received, func(i, j int) bool { return view.Received[i].FromNode < view.Received[j].FromNode })
		sort.Strings(view.Expected)
		detail.JoinBuckets = append(detail.JoinBuckets, view)
	}
	sort.Slice(detail.JoinBuckets, func(i, j int) bool {
		if detail.JoinBuckets[i].NodeID == detail.JoinBuckets[j].NodeID {
			return detail.JoinBuckets[i].ActivationID < detail.JoinBuckets[j].ActivationID
		}
		return detail.JoinBuckets[i].NodeID < detail.JoinBuckets[j].NodeID
	})
	for key, count := range rs.LoopCounters {
		detail.LoopCounters = append(detail.LoopCounters, LoopCounterView{EdgeID: key.EdgeID, ActivationID: key.ActivationID, Count: count})
	}
	sort.Slice(detail.LoopCounters, func(i, j int) bool {
		if detail.LoopCounters[i].EdgeID == detail.LoopCounters[j].EdgeID {
			return detail.LoopCounters[i].ActivationID < detail.LoopCounters[j].ActivationID
		}
		return detail.LoopCounters[i].EdgeID < detail.LoopCounters[j].EdgeID
	})
	return detail
}
