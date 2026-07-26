// Workflow types — mirrors Go types from internal/workflow/schema.go

// ─── Workflow Definition Types ──────────────────────────────────────────

export interface WorkflowMeta {
  name: string
  description: string
  version: string
  valid: boolean
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
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'

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
}

export interface WorkflowRunDetail extends WorkflowRunSummary {
  node_runs: NodeRunDTO[]
  terminal_outputs: TerminalOutput[]
  edges: WorkflowEdge[]
}
