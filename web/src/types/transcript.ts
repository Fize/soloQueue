// ─── Session Transcript Protocol ─────────────────────────────────────────────
//
// Mirrors internal/transcript. Every visible conversation change of a session
// is a frame with a session-monotonic `seq`. Items render in (seq, sub) order;
// a client resumes after the last applied seq and rebuilds from a snapshot whose
// watermark is consistent with the frames after it.

import type { CompactionStatus } from './chat'

export interface TranscriptFile {
  name: string
  path: string
}

export interface TranscriptInputPart {
  request_id: string
  item_id: string
  text: string
  ts?: string
}

export type TranscriptSegmentType = 'content' | 'thinking' | 'tool_call' | 'compact' | 'error'

export interface TranscriptSegment {
  type: TranscriptSegmentType
  text?: string
  call_id?: string
  name?: string
  args?: string
  result?: string
  error?: string
  duration_ms?: number
  done?: boolean
  dispatch_id?: string
  agent_instance_id?: string
}

export type TranscriptFrameKind =
  | 'input_queued'
  | 'input_committed'
  | 'input_dropped'
  | 'request_started'
  | 'request_route'
  | 'item_delta'
  | 'item_snapshot'
  | 'tool_started'
  | 'tool_finished'
  | 'dispatch_terminal'
  | 'delegation_started'
  | 'delegation_finished'
  | 'item_done'
  | 'request_end'
  | 'compaction_status'
  | 'session_meta'
  | 'transcript_reset'

export interface TranscriptFrame {
  seq: number
  ts: string
  kind: TranscriptFrameKind
  request_id?: string
  request_ids?: string[]
  item_id?: string
  text?: string
  channel?: 'content' | 'reasoning'
  parts?: TranscriptInputPart[]
  files?: TranscriptFile[]
  image_count?: number
  received_at?: string
  origin?: string
  call_id?: string
  name?: string
  args?: string
  result?: string
  error?: string
  duration_ms?: number
  dispatch_id?: string
  target_agent_id?: string
  agent_name?: string
  revision?: number
  status?: string
  num_tasks?: number
  segments?: TranscriptSegment[]
  ephemeral?: boolean
  task_type?: string
  model_id?: string
  provider_id?: string
  agent_instance_id?: string
  terminal_code?: string
  reason?: string
  session_name?: string
  plans?: string[]
  compaction?: CompactionStatus
}

export type TranscriptItemKind = 'user' | 'assistant' | 'error'

export interface TranscriptItem {
  id: string
  kind: TranscriptItemKind
  request_id?: string
  seq: number
  sub?: number
  last_seq?: number
  ts: string
  segments: TranscriptSegment[]
  files?: TranscriptFile[]
  image_count?: number
  done?: boolean
  ephemeral?: boolean
}

export interface TranscriptPendingInput {
  request_id: string
  text: string
  files?: TranscriptFile[]
  image_count?: number
  received_at?: string
  origin?: string
  seq: number
}

export interface TranscriptRequestState {
  request_id: string
  request_ids?: string[]
  origin?: string
  task_type?: string
  model_id?: string
  provider_id?: string
  agent_instance_id?: string
  delegating?: boolean
  ended?: boolean
  terminal_code?: string
  error?: string
  started_seq?: number
  ended_seq?: number
}

/** GET /api/session/transcript */
export interface TranscriptSnapshot {
  session_id: string
  epoch: string
  seq: number
  items: TranscriptItem[]
  pending: TranscriptPendingInput[]
  requests: TranscriptRequestState[]
  has_more: boolean
  cursor?: string
  compactions: CompactionStatus[]
  ctxwin_used: number
  ctxwin_limit: number
}

// Server → client WebSocket messages of the transcript protocol.

export interface WSFramesMessage {
  type: 'frames'
  session_id: string
  epoch: string
  frames: TranscriptFrame[]
}

export interface WSResyncRequired {
  type: 'resync_required'
  session_id: string
  epoch: string
  head_seq: number
  reason: string
}

export interface WSChatAck {
  type: 'chat_ack'
  request_id: string
  session_id?: string
  status: 'accepted' | 'queued' | 'rejected'
  error?: string
}

export interface WSCommandResult {
  type: 'command_result'
  request_id: string
  session_id?: string
  content: string
}

// Client → server subscription control.

export interface ClientSubscribe {
  type: 'subscribe'
  session_id: string
  epoch?: string
  after_seq?: number
}

export interface ClientUnsubscribe {
  type: 'unsubscribe'
  session_id: string
}
