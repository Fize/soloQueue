// ─── WebSocket Message Types ────────────────────────────────────────────────

import type { RuntimeStatus, AgentListResponse } from './agent'
import type {
  ClientSubscribe,
  ClientUnsubscribe,
  WSChatAck,
  WSCommandResult,
  WSFramesMessage,
  WSResyncRequired,
} from './transcript'

export interface WSStateMessage {
  type: "state";
  runtime?: RuntimeStatus;
  agents?: AgentListResponse;
}

export interface WSCancelConfirmed {
  type: "chat_cancel_confirmed";
  request_id: string;
  session_id: string;
}

export interface WSConnected {
  type: "connected";
}

export interface WSPong {
  type: "pong";
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

export interface CompactionStatus {
  id: string;
  status: "running" | "completed" | "failed" | "interrupted";
  reason?: string;
  started_at: string;
  finished_at?: string;
  error?: string;
  /** Browser-only deadline; history snapshots must never create an error notice. */
  noticeExpiresAt?: number;
  /** Browser-only ownership for suppressing the matching /compact error message. */
  requestId?: string;
}

/**
 * Messages the server sends. The conversation itself arrives only as ordered
 * transcript frames; every other message is runtime state or a reply to this
 * client.
 */
export type WSMessage =
  | WSFramesMessage
  | WSResyncRequired
  | WSChatAck
  | WSCommandResult
  | WSCancelConfirmed
  | WSStateMessage
  | WSNotificationMessage
  | WSConnected
  | WSPong;

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

export type ClientMessage =
  | ClientChatSend
  | ClientChatCancel
  | ClientSubscribe
  | ClientUnsubscribe;

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
  usage?: SessionUsage;
  plans?: string[];
}

export interface SessionUsage {
  current_segment: number;
  lifetime: SessionUsageTotals;
  segments?: Array<{ index: number; totals: SessionUsageTotals }>;
}

export interface SessionUsageTotals {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cache_hit_tokens: number;
  cache_miss_tokens: number;
  cache_input_tokens: number;
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
  queued?: boolean;
  queueError?: boolean;
  queueCancelled?: boolean;
  queuedImageCount?: number;
  id: string;
  /** Stable backend request ownership used to reconcile history with live frames. */
  requestId?: string;
  /** Page-lifetime worked preferences carried through history hydration; never persisted. */
  workedStateKeys?: Record<string, string>;
  role: "user" | "assistant";
  segments: ChatSegment[];
  timestamp: string;
  files?: { name: string; path: string }[];
  /** The owning request is still producing output for this bubble. */
  running?: boolean;
  /** The request resumed after other items; this bubble continues its reply. */
  continuation?: boolean;
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
      dispatchId?: string;
    }
  | {
      type: "delegation";
      agentName: string;
      task: string;
      status: "running" | "completed" | "failed" | "cancelled";
      durationMs?: number;
      resultContent?: string;
    }
  | { type: "error"; text: string };

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
  revision: string;
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
