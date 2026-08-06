// ─── WebSocket Message Types ────────────────────────────────────────────────

import type { RuntimeStatus, AgentListResponse } from './agent'
import type { SimulationEvent, SimulationProgress } from './simulation'

export interface WSStateMessage {
  type: "state";
  runtime: RuntimeStatus;
  agents: AgentListResponse;
}

export interface WSSimulationEventMessage {
  type: "simulation_event";
  event: SimulationEvent;
}

export interface WSSimulationProgressMessage {
  type: "simulation_progress";
  progress: SimulationProgress;
}

// Chat streaming messages (server → client)
export interface WSChatChunk {
  type: "chat_chunk";
  request_id: string;
  delta: string;
}

export interface WSReasoningChunk {
  type: "reasoning_chunk";
  request_id: string;
  delta: string;
}

export interface WSChatRoute {
  type: "chat_route";
  request_id: string;
  session_id: string;
	task_type: string;
  model_id: string;
  provider_id?: string;
  agent_instance_id?: string;
}

export interface WSToolStart {
  type: "tool_start";
  request_id: string;
  call_id: string;
  name: string;
  args: string;
  target_agent_id?: string;
}

export interface WSToolDone {
  type: "tool_done";
  request_id: string;
  call_id: string;
  name: string;
  result: string;
  error: string;
  duration_ms: number;
}

export interface WSToolConfirm {
  type: "tool_confirm";
  request_id: string;
  call_id: string;
  name: string;
  prompt: string;
  allow_in_session: boolean;
}

export interface WSChatDone {
  type: "chat_done";
  request_id: string;
  content: string;
  reasoning_content: string;
}

export interface WSChatError {
  type: "chat_error";
  request_id: string;
  error: string;
}

export interface WSChatQueued {
  type: "chat_queued";
  request_id: string;
  session_id?: string;
  error?: string;
}

export interface WSDelegationStart {
  type: "delegation_start";
  request_id: string;
  num_tasks: number;
}

export interface WSDelegationDone {
  type: "delegation_done";
  request_id: string;
  target_agent_id: string;
  agent_name?: string;
  duration_ms?: number;
  result_content?: string;
}

export interface WSSessionName {
  type: "session_name";
  request_id: string;
  name: string;
}

export interface WSConnected {
  type: "connected";
}

export interface WSPong {
  type: "pong";
}

export interface WSSessionPlans {
  type: "session_plans";
  request_id?: string;
  plans: string[];
}

// ─── Desktop Notification ────────────────────────────────────────────────────

export interface NotificationPayload {
  category: string;   // "cron", "system", "agent", ...
  level:    "info" | "success" | "warning" | "error";
  title:    string;
  body:     string;
  timestamp: string;  // ISO8601
}

export interface WSNotificationMessage {
  type: "notification";
  notification: NotificationPayload;
}

export type WSMessage =
  | WSStateMessage
  | WSSimulationEventMessage
  | WSSimulationProgressMessage
  | WSChatRoute
  | WSChatChunk
  | WSReasoningChunk
  | WSToolStart
  | WSToolDone
  | WSToolConfirm
  | WSChatDone
  | WSChatError
  | WSChatQueued
  | WSDelegationStart
  | WSDelegationDone
  | WSSessionName
  | WSSessionPlans
  | WSConnected
  | WSPong
  | WSNotificationMessage;

// Client → server messages
export interface ClientChatSend {
  type: "chat_send";
  request_id: string;
  session_id: string;
  prompt: string;
  files?: { name: string; path: string }[];
  design_mode?: boolean;
  selected_element?: any;
  active_design_file?: string;
  has_drawings?: boolean;
}

export interface ClientChatCancel {
  type: "chat_cancel";
  request_id: string;
  session_id: string;
}

export interface ClientToolConfirm {
  type: "tool_confirm";
  call_id: string;
  choice: string;
  session_id: string;
}

export type ClientMessage =
  | ClientChatSend
  | ClientChatCancel
  | ClientToolConfirm;

// ─── Chat Types ────────────────────────────────────────────────────────────

export interface ChatSession {
  id: string;
  type: "l1" | "l2";
  name: string;
  group?: string;
  agent_name?: string;
  agent_instance_id?: string;
  project_path?: string;
  design_dir?: string;
  createdAt: string;
  ctxwin_used?: number;
  ctxwin_limit?: number;
  plans?: string[];
}

export interface ChatRouteInfo {
  requestId: string;
  sessionId: string;
  taskLevel: string;
  modelId: string;
  providerId?: string;
  agentInstanceId?: string;
}

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  segments: ChatSegment[];
  timestamp: string;
  files?: { name: string; path: string }[];
}

export type ChatSegment =
  | { type: "thinking"; text: string }
  | { type: "content"; text: string }
  | { type: "compact"; text: string }
  | {
      type: "tool_call";
      callId: string;
      name: string;
      args: string;
      result?: string;
      error?: string;
      durationMs?: number;
      done: boolean;
      agentInstanceId?: string;
    }
  | {
      type: "delegation";
      agentName: string;
      task: string;
      status: "running" | "completed" | "failed" | "cancelled";
      durationMs?: number;
      resultContent?: string;
    }
  | { type: "error"; text: string }
  | {
      type: "tool_confirm";
      callId: string;
      name: string;
      prompt: string;
      allowInSession: boolean;
      resolved: boolean;
      choice?: string;
    };

export interface SessionListResponse {
  sessions: ChatSession[];
}

export interface CreateL2SessionResponse {
  id: string;
  name: string;
  group: string;
  agent_name: string;
  project_path?: string;
  design_dir?: string;
  created_at: string;
  plans?: string[];
}

export interface SessionHistorySegment {
  type:
    | "content"
    | "thinking"
    | "tool_call"
    | "delegation"
    | "error"
    | "tool_confirm"
    | "compact";
  text?: string;
  call_id?: string;
  name?: string;
  args?: string;
  result?: string;
  error?: string;
  duration_ms?: number;
  done?: boolean;
  status?: string;
  agent_name?: string;
  task?: string;
  prompt?: string;
  allow_in_session?: boolean;
  resolved?: boolean;
  choice?: string;
}

export interface SessionHistoryMessage {
  id: string;
  role: "user" | "assistant";
  segments: SessionHistorySegment[];
  timestamp: string;
  files?: { name: string; path: string }[];
}

export interface SessionHistoryResponse {
  messages: SessionHistoryMessage[];
  has_more: boolean;
  cursor?: string;
  ctxwin_used?: number;
  ctxwin_limit?: number;
}

// ─── Session Changes / Diff Types ───────────────────────────────────────────

export interface DiffLine {
  type: "add" | "del" | "ctx";
  content: string;
  old_num?: number;
  new_num?: number;
}

export interface DiffHunk {
  old_start: number;
  old_lines: number;
  new_start: number;
  new_lines: number;
  lines: DiffLine[];
}

export interface FileChange {
  path: string;
  status: "added" | "modified" | "deleted" | "renamed";
  old_path?: string;
  additions: number;
  deletions: number;
  binary: boolean;
  size_bytes?: number;
  hunks?: DiffHunk[];
}

export interface ChangesResponse {
  changes: FileChange[];
  total_additions: number;
  total_deletions: number;
  is_git_repo: boolean;
  base_ref?: string;
  plans?: string[];
}
