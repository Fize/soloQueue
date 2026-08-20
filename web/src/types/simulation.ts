// ─── Simulation Types ────────────────────────────────────────────────────────

export interface SimulationPersona {
  id: string;
  name: string;
  role: string;
  traits: Record<string, string>;
  system_prompt: string;
  goals?: string[];
  mbti?: string;
  age?: number;
  gender?: string;
  country?: string;
  profession?: string;
  bio?: string;
  persona?: string;
  model_id?: string;
  provider_id?: string;
}

export interface InitialRelationship {
  subject_name: string;
  target_name: string;
  kind: RelationKind;
  familiarity?: number;
  affinity?: number;
}

export interface SimulationRunConfig {
  id?: string;
  topic: string;
  personas: SimulationPersona[];
  max_wall_clock_ms?: number;
  simulated_hours?: number;
  tick_interval_ms?: number;
  time_scale?: number;
  enable_reflection?: boolean;
  initial_relationships?: InitialRelationship[];
  language?: string;
}

export interface SimulationMessage {
  agent_id: string;
  agent_name: string;
  content: string;
  reasoning?: string;
  to: string;
  type: string;
  round: number;
  seq_num: number;
}

export interface SimulationRelationEdge {
  source: string;
  target: string;
  type: string;
  weight: number;
}

export interface SimulationRelationGraph {
  nodes: string[];
  edges: SimulationRelationEdge[];
}

// ─── Relationship Types ───────────────────────────────────────────────────

export type RelationKind =
  | "parent"
  | "child"
  | "sibling"
  | "spouse"
  | "friend"
  | "rival"
  | "colleague"
  | "mentor"
  | "mentee"
  | "neighbor"
  | "stranger";

export interface RelationshipDTO {
  subject_id: string;
  subject_name: string;
  target_id: string;
  target_name: string;
  kind: string;
  familiarity: number;
  affinity: number;
  tags?: string[];
}

export interface PlanItem {
  start_time: string;
  end_time: string;
  activity: string;
  location: string;
  description: string;
  status: "pending" | "in_progress" | "completed" | "cancelled";
}

export interface MemoryRecord {
  round: number;
  role: string;
  content: string;
  world_state?: Record<string, any>;
  received_msgs?: any[];
  timestamp: string;
  record_type?: string;
  importance?: number;
  source?: string;
  location?: string;
  simulated_time?: string;
}

// ─── Simulation State ─────────────────────────────────────────────────────

export interface SimulationState {
  id: string;
  status:
    | "pending"
    | "idle"
    | "running"
    | "completed"
    | "failed"
    | "paused"
    | "cancelled";
  config: SimulationRunConfig;
  current_round: number;
  messages: SimulationMessage[];
  report?: string;
  graph?: SimulationRelationGraph;
  relationships?: RelationshipDTO[];
  started_at?: string;
  completed_at?: string;
  error?: string;
}

export interface SimulationEvent {
  type: string;
  simulation_id: string;
  round: number;
  data?: any;
  error?: string;
  timestamp: string;
}

export interface AgentProgressState {
  persona_id: string;
  name: string;
  role: string;
  message_count: number;
  last_action_type: string;
  last_action_time: string;
  status: "thinking" | "spoke" | "idle";
}

export interface GraphEdgeDTO {
  source: string;
  target: string;
  type: string;
  weight: number;
}

export interface SimulationProgress {
  simulation_id: string;
  phase:
    | "initializing"
    | "generating_plans"
    | "building_prompts"
    | "running"
    | "generating_report"
    | "completed"
    | "failed"
    | "paused";
  progress_percent: number;
  current_actions: number;
  max_actions: number;
  elapsed_seconds: number;
  estimated_remaining_seconds: number;
  agent_states: Record<string, AgentProgressState>;
  graph_edges: GraphEdgeDTO[];
  relationship_edges?: RelationshipDTO[];
  recent_logs: string[];
}
