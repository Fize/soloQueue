// Workflow types — mirrors Go types from internal/workflow/schema.go

// ─── Workflow Definition Types ──────────────────────────────────────────

export interface WorkflowMeta {
  name: string
  description: string
  version: string
  valid: boolean
  draft?: boolean
  error?: string
}

export interface Defaults {
  node_timeout: string // e.g. "20m"
  workflow_timeout: string // e.g. "45m"
  max_node_runs: number
  max_output_bytes: number
}

export interface AgentRef {
  template: string
  model: string
}

export interface ErrorPolicy {
  strategy: 'fail' | 'retry'
  max_attempts: number
}

export interface OutputDef {
  to: string[]
  loop: boolean
  max_traversals: number
  terminal_status?: 'completed' | 'blocked' | 'failed'
}

export interface JoinDef {
  mode: 'all'
  from: string[]
}

export interface NodeDef {
  id: string
  agent: string
  prompt: string
  timeout?: string
  join?: JoinDef
  outputs: Record<string, OutputDef>
  on_error?: ErrorPolicy
}

export interface WorkflowDef {
  name: string
  description: string
  version: string
  defaults: Defaults
  agents: Record<string, AgentRef>
  entry: string[]
  nodes: NodeDef[]
}

export interface WorkflowEdge {
  from_node: string
  outcome: string
  to_node: string
  loop: boolean
  max_traversals: number
  terminal_status?: 'completed' | 'blocked' | 'failed'
}

// ─── Graph State (internal editor model) ────────────────────────────────

export interface GraphNode {
  id: string
  agent: string
  prompt: string
  timeout?: string
  join?: JoinDef
  outputs: Record<string, OutputDef>
  onError?: ErrorPolicy
  position: { x: number; y: number }
}

export interface GraphEdge {
  id: string
  source: string
  target: string
  outcome: string
  loop: boolean
  maxTraversals: number
}

export interface GraphState {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

// ─── Execution State Types ──────────────────────────────────────────────

export type NodeRunState =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | 'timed_out'

export type RunStatus =
  | 'pending'
  | 'preparing_worktree'
  | 'running'
  | 'pause_requested'
  | 'paused'
  | 'resuming'
  | 'interrupted'
  | 'completed'
  | 'blocked'
  | 'failed'
  | 'cancelled'
  | 'abandoned'

export interface WorkflowDeliveryRequest {
  commit?: { enabled: boolean; message?: string }
  push?: { enabled: boolean; remote?: string; branch?: string }
  pull_request?: { enabled: boolean; title?: string; body?: string; draft?: boolean }
}

export interface WorkflowTask {
  goal: string
  acceptance_criteria: string[]
  constraints?: string[]
  delivery?: WorkflowDeliveryRequest
}

export interface BuiltinWorkflowView {
  spec: { name: string; description: string; version: string; yaml: string }
  status: 'available' | 'installed' | 'conflict'
  error?: string
}

export interface WorkflowRunEvent {
  id: number
  run_id: string
  node_run_id?: string
  type: string
  payload: Record<string, unknown>
  prev_hash?: string
  hash: string
  created_at: string
}

export interface WorkflowConfirmation {
  call_id: string
  node_run_id?: string
  tool_name?: string
  prompt: string
  options?: string[]
  allow_in_session: boolean
  status: string
  choice?: string
  requested_at: string
  resolved_at?: string
}

export interface NodeInputDTO {
  from_node: string
  outcome: string
  content: string
  activation_id: string
}

export interface HandoffData {
  outcome: string
  content: string
}

export interface NodeRunDTO {
  id: string
  node_id: string
  attempt: number
  activation_id: string
  state: NodeRunState
  inputs: NodeInputDTO[]
  result?: HandoffData
  error?: string
  started_at: string
  finished_at: string
}

export interface TerminalOutput {
  node: string
  outcome: string
  content: string
}

export interface WorkflowRunSummary {
  id: string
  workflow_name: string
  status: RunStatus
  started_at: string
  finished_at?: string
  input: string
  node_count: number
  completed_count: number
  failed_count: number
  task: WorkflowTask
  source?: string
  repository_path?: string
  base_ref?: string
  base_commit?: string
  branch_name?: string
  worktree_path?: string
  worktree_state?: string
  parent_run_id?: string
  restarted_from_run_id?: string
  successor_run_id?: string
  pause_mode?: string
  resume_available: boolean
  restart_available: boolean
  cleanup_available: boolean
  quality_status?: string
  delivery_status?: string
  delivery_result?: Record<string, unknown>
  audit_dir?: string
  audit_head_hash?: string
  error_code?: string
  error_message?: string
}

export interface WorkflowRunDetail extends WorkflowRunSummary {
  node_runs: NodeRunDTO[]
  terminal_outputs: TerminalOutput[]
  edges: WorkflowEdge[]
  confirmations?: WorkflowConfirmation[]
}
