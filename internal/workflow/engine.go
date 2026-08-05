package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Activation ID
// ---------------------------------------------------------------------------

// newActivationID generates a short unique activation identifier.
func newActivationID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newRunID generates a workflow run identifier.
func newRunID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "wf_" + hex.EncodeToString(b)
}

// newNodeRunID generates a NodeRun ID.
func newNodeRunID(nodeID string, attempt int) string {
	return fmt.Sprintf("%s_%d_%s", nodeID, attempt, newActivationID())
}

// ---------------------------------------------------------------------------
// Edge key helpers (for loop counting)
// ---------------------------------------------------------------------------

func edgeKey(from, outcome, to string) string {
	return fmt.Sprintf("%s|%s|%s", from, outcome, to)
}

func makeLoopKey(edgeKey, activationID string) LoopKey {
	return LoopKey{EdgeID: edgeKey, ActivationID: activationID}
}

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

// Engine executes workflow definitions using a NodeExecutor.
type Engine struct {
	executor NodeExecutor
	limits   EngineLimits
}

// RunOptions configures an execution without exposing the engine's mutable
// scheduler internals to callers. Observer is invoked by the scheduler
// goroutine and must return promptly.
type RunOptions struct {
	ID                 string
	Observer           func(*RunState)
	PauseRequested     func() (mode string, requested bool)
	CancelRequested    func() bool
	RecordConfirmation func(ConfirmationRequest)
	Resume             *ResumeInput
}

type ResumeInput struct {
	NodeRuns       []*NodeRun
	ReadyQueue     []string
	JoinBuckets    map[JoinKey]*JoinBucket
	LoopCounters   map[LoopKey]int
	TerminalOutput []TerminalOutput
}

// NewEngine creates a workflow engine with the given executor and limits.
func NewEngine(executor NodeExecutor, limits EngineLimits) *Engine {
	if limits.MaxYAMLBytes == 0 {
		limits = DefaultEngineLimits()
	}
	return &Engine{executor: executor, limits: limits}
}

// Run executes a workflow synchronously, blocking until completion or error.
// The input is passed to each NodeRun as workflow-level context.
func (e *Engine) Run(ctx context.Context, wf *ParsedWorkflow, input string, workDir string) (*RunState, error) {
	return e.RunWithOptions(ctx, wf, input, workDir, RunOptions{})
}

// RunWithOptions executes a workflow and publishes scheduler-safe snapshots to
// an optional observer. The RunState passed to Observer must not be retained or
// mutated; consumers should copy the fields they need before returning.
func (e *Engine) RunWithOptions(ctx context.Context, wf *ParsedWorkflow, input string, workDir string, options RunOptions) (*RunState, error) {
	// Clamp workflow defaults to engine limits
	defaults := e.limits.Clamp(wf.Defaults)

	// Create run-level context with workflow timeout
	runCtx, runCancel := context.WithTimeout(ctx, defaults.WorkflowTimeout.Duration())
	defer runCancel()

	runID := options.ID
	if runID == "" {
		runID = newRunID()
	}
	rs := &RunState{
		ID:           runID,
		Workflow:     wf,
		Status:       RunRunning,
		NodeRuns:     make(map[string]*NodeRun),
		Running:      make(map[string]contextCanceller),
		JoinBuckets:  make(map[JoinKey]*JoinBucket),
		LoopCounters: make(map[LoopKey]int),
		StartedAt:    time.Now(),
		Input:        input,
		WorkDir:      workDir,
	}

	resumeHasState := options.Resume != nil && (len(options.Resume.NodeRuns) > 0 || len(options.Resume.ReadyQueue) > 0 || len(options.Resume.JoinBuckets) > 0 || len(options.Resume.LoopCounters) > 0 || len(options.Resume.TerminalOutput) > 0)
	if resumeHasState {
		for _, nodeRun := range options.Resume.NodeRuns {
			if nodeRun == nil {
				continue
			}
			copy := *nodeRun
			copy.Inputs = append([]NodeInput(nil), nodeRun.Inputs...)
			rs.NodeRuns[copy.ID] = &copy
		}
		rs.ReadyQueue = append(rs.ReadyQueue, options.Resume.ReadyQueue...)
		for key, bucket := range options.Resume.JoinBuckets {
			if bucket == nil {
				continue
			}
			copy := &JoinBucket{Received: make(map[string]NodeInput, len(bucket.Received)), Expected: make(map[string]bool, len(bucket.Expected))}
			for source, input := range bucket.Received {
				copy.Received[source] = input
			}
			for source, expected := range bucket.Expected {
				copy.Expected[source] = expected
			}
			rs.JoinBuckets[key] = copy
		}
		for key, count := range options.Resume.LoopCounters {
			rs.LoopCounters[key] = count
		}
		rs.TerminalOutput = append(rs.TerminalOutput, options.Resume.TerminalOutput...)
	} else {
		// Create root activation for entry nodes
		rootActivation := newActivationID()

		// Initialize entry NodeRuns — all share root activation
		for _, entryID := range wf.Entry {
			e.createNodeRun(rs, entryID, rootActivation, input)
		}
	}
	if options.Observer != nil {
		options.Observer(rs)
	}

	// Scheduling loop
	resultCh := make(chan nodeExecResult, 1)
	var wg sync.WaitGroup

	for {
		pauseMode := ""
		if options.PauseRequested != nil {
			pauseMode, _ = options.PauseRequested()
		}
		if pauseMode == "force" {
			rs.PauseMode = pauseMode
			rs.Status = RunPaused
			e.cancelAll(rs, fmt.Errorf("workflow force-paused"))
			break
		}
		if pauseMode == "graceful" && len(rs.Running) == 0 {
			rs.PauseMode = pauseMode
			rs.Status = RunPaused
			break
		}
		// A graceful pause lets in-flight nodes reach a result boundary, but
		// does not start another queued node while the pause is pending.
		// Drain any ready nodes up to concurrency limit
		if pauseMode != "graceful" {
			e.dispatchReady(rs, runCtx, resultCh, &wg, defaults, options.RecordConfirmation)
		}
		if options.Observer != nil {
			options.Observer(rs)
		}

		// If nothing is running and nothing is ready, we're done
		if len(rs.Running) == 0 && len(rs.ReadyQueue) == 0 {
			break
		}

		// Once the concurrency limit is full, queued work cannot make progress
		// until an in-flight node finishes, so always consume a result first.
		if len(rs.Running) > 0 {
			select {
			case result := <-resultCh:
				e.handleResult(rs, runCtx, result, resultCh, &wg, defaults)
				if options.Observer != nil {
					options.Observer(rs)
				}
			case <-runCtx.Done():
				e.cancelAll(rs, runCtx.Err())
				if options.PauseRequested != nil {
					if mode, requested := options.PauseRequested(); requested {
						rs.PauseMode = mode
						rs.Status = RunPaused
					}
				}
				if options.CancelRequested != nil && options.CancelRequested() {
					rs.Status = RunCancelled
				}
				if options.Observer != nil {
					options.Observer(rs)
				}
				break
			}
			continue
		}
	}

	// Wake executors that are still trying to publish after a terminal,
	// fail-fast, pause, or cancellation path has stopped the scheduler.
	runCancel()
	wg.Wait()

	// Determine final status
	e.finalizeStatus(rs)
	rs.FinishedAt = time.Now()
	if options.Observer != nil {
		options.Observer(rs)
	}
	return rs, nil
}

// ---------------------------------------------------------------------------
// NodeRun creation helpers
// ---------------------------------------------------------------------------

func (e *Engine) createNodeRun(rs *RunState, nodeID, activationID, input string) *NodeRun {
	if _, ok := rs.Workflow.Nodes[nodeID]; !ok {
		// Should not happen if validation passed
		return nil
	}

	// Check global node run limit
	if len(rs.NodeRuns) >= e.limits.MaxNodeRuns {
		rs.Status = RunFailed
		return nil
	}

	nr := &NodeRun{
		ID:           newNodeRunID(nodeID, 0),
		NodeID:       nodeID,
		Attempt:      1,
		ActivationID: activationID,
		State:        NodeQueued,
		StartedAt:    time.Time{},
	}

	// Collect upstream inputs for this activation
	var inputs []NodeInput
	for _, other := range rs.NodeRuns {
		if other.ActivationID == activationID && other.State == NodeSucceeded && other.Result != nil {
			inputs = append(inputs, NodeInput{
				FromNode:     other.NodeID,
				Outcome:      other.Result.Outcome,
				Content:      other.Result.Content,
				ActivationID: other.ActivationID,
			})
		}
	}

	// Also add workflow-level input for entry nodes
	isEntry := false
	for _, eid := range rs.Workflow.Entry {
		if eid == nodeID {
			isEntry = true
			break
		}
	}
	if isEntry && len(inputs) == 0 {
		// Entry node: the workflow input is the only upstream data
	}

	nr.Inputs = inputs
	rs.NodeRuns[nr.ID] = nr
	rs.ReadyQueue = append(rs.ReadyQueue, nr.ID)
	return nr
}

// createRetryNodeRun creates a retry NodeRun for on_error retry.
func (e *Engine) createRetryNodeRun(rs *RunState, failed *NodeRun) *NodeRun {
	nr := &NodeRun{
		ID:           newNodeRunID(failed.NodeID, failed.Attempt),
		NodeID:       failed.NodeID,
		Attempt:      failed.Attempt + 1,
		ActivationID: failed.ActivationID,
		State:        NodeQueued,
		Inputs:       failed.Inputs,
	}
	rs.NodeRuns[nr.ID] = nr
	rs.ReadyQueue = append(rs.ReadyQueue, nr.ID)
	return nr
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

type nodeExecResult struct {
	NodeRunID string
	Handoff   *HandoffData
	Err       error
}

func (e *Engine) dispatchReady(rs *RunState, ctx context.Context, resultCh chan<- nodeExecResult, wg *sync.WaitGroup, defaults Defaults, recordConfirmation func(ConfirmationRequest)) {
	maxParallel := e.limits.MaxParallelNodes
	if maxParallel <= 0 {
		maxParallel = 4
	}

	for len(rs.ReadyQueue) > 0 && len(rs.Running) < maxParallel {
		nrID := rs.ReadyQueue[0]
		rs.ReadyQueue = rs.ReadyQueue[1:]

		nr, ok := rs.NodeRuns[nrID]
		if !ok || nr.State != NodeQueued {
			continue
		}

		nr.State = NodeRunning
		nr.StartedAt = time.Now()

		// Determine per-node timeout
		nodeDef := rs.Workflow.Nodes[nr.NodeID]
		nodeTimeout := defaults.NodeTimeout.Duration()
		if nodeDef.Timeout > 0 {
			nodeTimeout = nodeDef.Timeout.Duration()
		}

		nodeCtx, nodeCancel := context.WithTimeout(ctx, nodeTimeout)
		rs.Running[nrID] = func() { nodeCancel() }

		wg.Add(1)
		go func(nrID string) {
			defer wg.Done()

			nr := rs.NodeRuns[nrID]
			node := rs.Workflow.Nodes[nr.NodeID]
			agentRef := rs.Workflow.Agents[node.Agent]

			req := NodeRunRequest{
				RunID:              rs.ID,
				Workflow:           rs.Workflow,
				Node:               node,
				AgentRef:           agentRef,
				NodeRun:            nr,
				WorkflowInput:      rs.Input,
				WorkDir:            rs.WorkDir,
				RecordConfirmation: recordConfirmation,
			}

			result, err := e.executor.Execute(nodeCtx, req)
			nodeCancel()

			// Send result back to scheduler
			select {
			case resultCh <- nodeExecResult{
				NodeRunID: nrID,
				Handoff:   result.Handoff,
				Err:       err,
			}:
			case <-ctx.Done():
			}
		}(nrID)
	}
}

// ---------------------------------------------------------------------------
// Result handling
// ---------------------------------------------------------------------------

func (e *Engine) handleResult(rs *RunState, ctx context.Context, result nodeExecResult, resultCh chan<- nodeExecResult, wg *sync.WaitGroup, defaults Defaults) {
	nr, ok := rs.NodeRuns[result.NodeRunID]
	if !ok {
		return
	}

	// Remove from running
	cancelFn, wasRunning := rs.Running[result.NodeRunID]
	if wasRunning {
		cancelFn() // cleanup cancel func
		delete(rs.Running, result.NodeRunID)
	}

	// Check for context cancellation in between
	if ctx.Err() != nil {
		nr.State = NodeCancelled
		nr.Error = ctx.Err()
		nr.FinishedAt = time.Now()
		return
	}

	if result.Err != nil {
		// System error — apply on_error policy
		e.handleNodeError(rs, ctx, nr, result.Err, resultCh, wg, defaults)
		return
	}

	// Successful execution with handoff
	if result.Handoff == nil {
		// Missing handoff
		nr.State = NodeFailed
		nr.Error = fmt.Errorf("HANDOFF_MISSING")
		nr.FinishedAt = time.Now()
		e.failFast(rs, ctx, "handoff missing")
		return
	}

	nr.State = NodeSucceeded
	nr.Result = result.Handoff
	nr.FinishedAt = time.Now()

	// Look up the output for this outcome
	out, ok := rs.Workflow.Nodes[nr.NodeID].Outputs[result.Handoff.Outcome]
	if !ok {
		// Unknown outcome
		nr.State = NodeFailed
		nr.Error = fmt.Errorf("HANDOFF_OUTCOME_UNKNOWN: %s", result.Handoff.Outcome)
		nr.FinishedAt = time.Now()
		e.failFast(rs, ctx, fmt.Sprintf("unknown outcome: %s", result.Handoff.Outcome))
		return
	}

	// Handle output routing
	if len(out.To) == 0 {
		// Terminal output
		rs.TerminalOutput = append(rs.TerminalOutput, TerminalOutput{
			Node:    nr.NodeID,
			Outcome: result.Handoff.Outcome,
			Content: result.Handoff.Content,
		})
		switch out.TerminalStatus {
		case "blocked":
			rs.Status = RunBlocked
			e.cancelAll(rs, fmt.Errorf("workflow terminal outcome blocked"))
		case "failed":
			rs.Status = RunFailed
			e.cancelAll(rs, fmt.Errorf("workflow terminal outcome failed"))
		}
		return
	}

	if out.Loop {
		// Loop edge — check limit
		key := makeLoopKey(edgeKey(nr.NodeID, result.Handoff.Outcome, out.To[0]), nr.ActivationID)
		count := rs.LoopCounters[key]
		if count >= out.MaxTraversals {
			nr.State = NodeFailed
			nr.Error = fmt.Errorf("LOOP_LIMIT_EXCEEDED: %d traversals", count)
			nr.FinishedAt = time.Now()
			e.failFast(rs, ctx, "loop limit exceeded")
			return
		}
		rs.LoopCounters[key] = count + 1

		// Create new NodeRun for the loop target
		// Inherit the current activation ID (same activation for loop counting)
		e.createNodeRun(rs, out.To[0], nr.ActivationID, rs.Input)
	} else {
		// Regular edge (possibly fan-out to multiple targets)
		for _, to := range out.To {
			e.routeToTarget(rs, to, nr, result.Handoff.Content)
		}
	}
}

func (e *Engine) routeToTarget(rs *RunState, targetNodeID string, source *NodeRun, content string) {
	targetNode := rs.Workflow.Nodes[targetNodeID]

	if targetNode.Join != nil {
		// Fan-in: add to join bucket
		joinKey := JoinKey{
			NodeID:       targetNodeID,
			ActivationID: source.ActivationID,
		}
		bucket, exists := rs.JoinBuckets[joinKey]
		if !exists {
			bucket = &JoinBucket{
				Received: make(map[string]NodeInput),
				Expected: make(map[string]bool),
			}
			for _, src := range targetNode.Join.From {
				bucket.Expected[src] = true
			}
			rs.JoinBuckets[joinKey] = bucket
		}

		bucket.Received[source.NodeID] = NodeInput{
			FromNode:     source.NodeID,
			Outcome:      source.Result.Outcome,
			Content:      content,
			ActivationID: source.ActivationID,
		}

		if bucket.IsSatisfied() {
			// All inputs received — create NodeRun for the join node
			delete(rs.JoinBuckets, joinKey)
			e.createNodeRun(rs, targetNodeID, source.ActivationID, rs.Input)
		}
	} else {
		// Regular node: create NodeRun directly
		e.createNodeRun(rs, targetNodeID, source.ActivationID, rs.Input)
	}
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func (e *Engine) handleNodeError(rs *RunState, ctx context.Context, nr *NodeRun, err error, resultCh chan<- nodeExecResult, wg *sync.WaitGroup, defaults Defaults) {
	// Check for timeout
	if ctx.Err() != nil {
		nr.State = NodeTimedOut
		nr.Error = fmt.Errorf("NODE_TIMEOUT: %w", ctx.Err())
		nr.FinishedAt = time.Now()
		e.failFast(rs, ctx, "node timeout")
		return
	}

	nodeDef := rs.Workflow.Nodes[nr.NodeID]
	policy := nodeDef.OnError
	if policy == nil {
		policy = &ErrorPolicy{Strategy: "fail"}
	}
	if policy.Strategy == "" {
		policy = &ErrorPolicy{Strategy: "fail"}
	}

	switch policy.Strategy {
	case "retry":
		maxAttempts := policy.MaxAttempts
		if maxAttempts < 2 {
			maxAttempts = 2
		}
		if nr.Attempt < maxAttempts {
			// Retry: create new NodeRun for same node
			nr.State = NodeFailed
			nr.Error = err
			nr.FinishedAt = time.Now()
			e.createRetryNodeRun(rs, nr)
			return
		}
		// Max attempts reached — fall through to fail
		fallthrough
	default: // "fail"
		nr.State = NodeFailed
		nr.Error = fmt.Errorf("NODE_EXECUTION_FAILED: %w", err)
		nr.FinishedAt = time.Now()
		e.failFast(rs, ctx, fmt.Sprintf("node %s failed", nr.NodeID))
	}
}

func (e *Engine) failFast(rs *RunState, ctx context.Context, reason string) {
	_ = reason
	rs.Status = RunFailed
	e.cancelAll(rs, fmt.Errorf("workflow failed: %s", reason))
}

func (e *Engine) cancelAll(rs *RunState, cause error) {
	for id, cancel := range rs.Running {
		cancel()
		if nr, ok := rs.NodeRuns[id]; ok && nr.State == NodeRunning {
			nr.State = NodeCancelled
			nr.Error = cause
			nr.FinishedAt = time.Now()
		}
	}
	// Clear queues
	for id := range rs.Running {
		delete(rs.Running, id)
	}
	rs.ReadyQueue = nil
}

// ---------------------------------------------------------------------------
// Final status determination
// ---------------------------------------------------------------------------

func (e *Engine) finalizeStatus(rs *RunState) {
	// If already set to failed/cancelled, keep it
	if rs.Status == RunFailed || rs.Status == RunCancelled || rs.Status == RunPaused || rs.Status == RunBlocked || rs.Status == RunAbandoned || rs.Status == RunInterrupted {
		return
	}

	// Check for unsatisfied joins
	for _, bucket := range rs.JoinBuckets {
		if !bucket.IsSatisfied() {
			rs.Status = RunFailed
			return
		}
	}

	// Check for terminal outputs
	if len(rs.TerminalOutput) > 0 {
		rs.Status = RunCompleted
		return
	}

	// No terminal output — check if all nodes completed
	allDone := true
	for _, nr := range rs.NodeRuns {
		if !nr.State.IsTerminal() {
			allDone = false
			break
		}
	}
	if allDone {
		rs.Status = RunFailed // NO_TERMINAL_PATH
	} else {
		rs.Status = RunFailed
	}
}
