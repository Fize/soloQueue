import { useEffect, useState, useRef, useCallback } from 'react'
import { Bot, X, Hash, AlertCircle, Terminal, Clock, Loader2 } from 'lucide-react'
import { AgentStateBadge } from './AgentStateBadge'

type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped'

interface Segment {
  type: 'thinking' | 'content' | 'tool_call'
  text?: string
  call_id?: string
  name?: string
  args?: string
  result?: string
  error?: string
  done?: boolean
  duration_ms?: number
}

interface AgentStreamState {
  agent_id: string
  processing: boolean
  segments: Segment[]
  iteration: number
  error?: string
}

interface AgentInfo {
  id: string
  instance_id: string
  name: string
  state: AgentState
  model_id: string
  provider_id: string
  group: string
  is_leader: boolean
  task_type: string
  last_level?: string
  error_count: number
  last_error: string
  iteration?: number
}

interface AgentModalProps {
  agent: AgentInfo
  stream: AgentStreamState | undefined
  onClose: () => void
  t: (key: string, v?: Record<string, string | number>) => string
}

export function AgentModal({ agent, stream, onClose, t }: AgentModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null)
  const [closing, setClosing] = useState(false)

  // Lock body scroll
  useEffect(() => {
    document.body.classList.add('modal-open')
    return () => document.body.classList.remove('modal-open')
  }, [])

  // Close on ESC
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') handleClose()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  })

  const handleClose = useCallback(() => {
    setClosing(true)
    setTimeout(() => onClose(), 150)
  }, [onClose])

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === overlayRef.current) handleClose()
  }

  const streamBodyRef = useRef<HTMLDivElement>(null)

  // Architectural Decision: Preserve chronological segment order and auto-scroll on stream updates.
  const segments = stream?.segments || []
  const hasStreamData = segments.length > 0

  useEffect(() => {
    if (streamBodyRef.current) {
      streamBodyRef.current.scrollTop = streamBodyRef.current.scrollHeight
    }
  }, [segments])

  return (
    <div
      ref={overlayRef}
      onClick={handleOverlayClick}
      className={`fixed inset-0 z-[100] flex items-center justify-center p-4 sm:p-6 ${closing ? 'modal-overlay-exiting' : 'modal-overlay-entering'}`}
      style={{
        backgroundColor: 'rgba(0,0,0,0.6)',
        backdropFilter: 'blur(4px)',
      }}
    >
      <div
        className={`w-full max-w-2xl max-h-[85vh] flex flex-col rounded-xl overflow-hidden shadow-2xl ${closing ? 'modal-content-exiting' : 'modal-content-entering'}`}
        style={{ backgroundColor: 'var(--color-card)' }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Modal header */}
        <div
          className="px-5 py-4 border-b flex items-center justify-between shrink-0"
          style={{ borderColor: 'var(--color-border)' }}
        >
          <div className="flex items-center gap-3 min-w-0">
            <div
              className="h-9 w-9 rounded-lg flex items-center justify-center shrink-0"
              style={{
                backgroundColor: 'color-mix(in srgb, var(--color-primary) 12%, transparent)',
                color: 'var(--color-primary)',
              }}
            >
              <Bot className="h-5 w-5" />
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-bold truncate" style={{ color: 'var(--color-foreground)' }}>
                {agent.name}
              </h2>
              {agent.is_leader && (
                <span
                  className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-bold mt-0.5"
                  style={{
                    backgroundColor: 'color-mix(in srgb, var(--color-warning) 12%, transparent)',
                    color: 'var(--color-warning)',
                  }}
                >
                  {t('table.badges.leader')}
                </span>
              )}
            </div>
          </div>
          <button
            onClick={handleClose}
            className="shrink-0 w-8 h-8 rounded-lg flex items-center justify-center cursor-pointer transition-colors"
            style={{
              color: 'var(--color-muted-foreground)',
            }}
            onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = 'var(--color-surface-secondary)' }}
            onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = 'transparent' }}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Agent info grid */}
        <div
          className="px-5 py-4 grid grid-cols-2 sm:grid-cols-4 gap-4 border-b shrink-0"
          style={{ borderColor: 'var(--color-border)' }}
        >
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.state')}
            </span>
            <AgentStateBadge state={agent.state} />
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.model')}
            </span>
            <span className="text-sm font-mono truncate" title={`${agent.provider_id}/${agent.model_id}`}>
              <span style={{ color: 'var(--color-muted-foreground)' }}>{agent.provider_id}/</span>
              <span style={{ color: 'var(--color-foreground)' }}>{agent.model_id}</span>
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.group')}
            </span>
            <span className="text-sm font-mono" style={{ color: 'var(--color-foreground)' }}>
              {agent.group || 'Global'}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.level')}
            </span>
            <span
              className="inline-flex items-center px-2 py-0.5 rounded text-xs font-mono w-fit"
              style={{
                backgroundColor: 'var(--color-surface-secondary)',
                color: 'var(--color-foreground)',
              }}
              title={agent.last_level ? t('modal.lastLevel', { level: agent.last_level }) : undefined}
            >
              {agent.task_type}
            </span>
          </div>
        </div>

        {/* Stats row */}
        <div
          className="px-5 py-3 flex items-center gap-6 border-b shrink-0"
          style={{ borderColor: 'var(--color-border)' }}
        >
          <span className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
            <Hash className="h-3.5 w-3.5" />
            {t('modal.iteration')}: <span className="font-semibold font-mono" style={{ color: 'var(--color-foreground)' }}>{stream?.iteration ?? (agent.iteration ?? 0)}</span>
          </span>
          <span className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
            <AlertCircle className="h-3.5 w-3.5" style={{ color: agent.error_count > 0 ? 'var(--color-destructive)' : undefined }} />
            {t('modal.errors')}: <span className="font-semibold font-mono" style={{ color: agent.error_count > 0 ? 'var(--color-destructive)' : 'var(--color-foreground)' }}>{agent.error_count}</span>
          </span>
        </div>

        {/* Error display */}
        {agent.last_error && (
          <div
            className="px-5 py-3 border-b shrink-0"
            style={{ borderColor: 'var(--color-border)', backgroundColor: 'color-mix(in srgb, var(--color-destructive) 8%, transparent)' }}
          >
            <span className="text-[11px] font-semibold block" style={{ color: 'var(--color-destructive)' }}>
              {t('modal.lastError')}:
            </span>
            <span className="text-xs font-mono whitespace-pre-wrap break-all mt-0.5 block" style={{ color: 'var(--color-destructive)' }}>
              {agent.last_error}
            </span>
          </div>
        )}

        {/* Live stream */}
        <div ref={streamBodyRef} className="flex-1 overflow-y-auto p-5">
          <div className="flex items-center gap-2 mb-3">
            <Terminal className="h-4 w-4" style={{ color: agent.state === 'processing' ? 'var(--color-signal)' : 'var(--color-muted-foreground)' }} />
            <span className="text-xs font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.streamTitle')}
            </span>
          </div>

          {agent.state !== 'processing' && !hasStreamData ? (
            <div className="flex flex-col items-center justify-center py-8 gap-2">
              <Clock className="h-5 w-5" style={{ color: 'var(--color-muted-foreground)' }} />
              <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
                {t('modal.idle')}
              </span>
            </div>
          ) : !hasStreamData ? (
            <div className="flex items-center gap-2 py-8 justify-center">
              <Loader2 className="h-4 w-4 animate-spin" style={{ color: 'var(--color-muted-foreground)' }} />
              <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
                {t('modal.noStream')}
              </span>
            </div>
          ) : (
            <div className="font-mono text-xs leading-relaxed space-y-3">
              {segments.map((seg, i) => {
                if (seg.type === 'thinking') {
                  return (
                    <div
                      key={`seg-${i}`}
                      className="border-l-2 pl-3"
                      style={{ borderColor: 'color-mix(in srgb, var(--color-signal) 50%, transparent)' }}
                    >
                      <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--color-signal)' }}>
                        {t('stream.thinking')}
                      </span>
                      <span className="whitespace-pre-wrap" style={{ color: 'var(--color-muted-foreground)' }}>
                        {seg.text}
                      </span>
                    </div>
                  )
                }
                if (seg.type === 'content') {
                  return (
                    <div
                      key={`seg-${i}`}
                      className="border-l-2 pl-3"
                      style={{ borderColor: 'color-mix(in srgb, var(--color-accent) 50%, transparent)' }}
                    >
                      <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--color-accent)' }}>
                        {t('stream.content')}
                      </span>
                      <span className="whitespace-pre-wrap" style={{ color: 'var(--color-foreground)' }}>
                        {seg.text}
                      </span>
                    </div>
                  )
                }
                if (seg.type === 'tool_call') {
                  return (
                    <div
                      key={`seg-${i}`}
                      className="border-l-2 pl-3"
                      style={{ borderColor: seg.error ? 'var(--color-destructive)' : 'color-mix(in srgb, var(--color-primary) 50%, transparent)' }}
                    >
                      <div className="flex items-center gap-2 mb-1 flex-wrap">
                        <span
                          className="font-semibold text-xs"
                          style={{ color: seg.error ? 'var(--color-destructive)' : 'var(--color-primary)' }}
                        >
                          {t('stream.toolCall')}: {seg.name}
                        </span>
                        {seg.done && !seg.error && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded" style={{
                            backgroundColor: 'color-mix(in srgb, var(--color-success) 12%, transparent)',
                            color: 'var(--color-success)',
                          }}>
                            {t('stream.toolDone')}
                          </span>
                        )}
                        {seg.error && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded" style={{
                            backgroundColor: 'color-mix(in srgb, var(--color-destructive) 12%, transparent)',
                            color: 'var(--color-destructive)',
                          }}>
                            {t('stream.toolError')}
                          </span>
                        )}
                        {seg.duration_ms !== undefined && (
                          <span className="text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
                            {t('stream.toolDuration', { ms: seg.duration_ms })}
                          </span>
                        )}
                      </div>
                      {seg.args && (
                        <details className="mt-1">
                          <summary className="text-[10px] cursor-pointer" style={{ color: 'var(--color-muted-foreground)' }}>
                            Arguments
                          </summary>
                          <pre className="text-[10px] mt-1 p-2 rounded overflow-x-auto" style={{
                            backgroundColor: 'var(--color-surface-secondary)',
                            color: 'var(--color-muted-foreground)',
                            maxHeight: '120px',
                          }}>
                            {seg.args}
                          </pre>
                        </details>
                      )}
                      {seg.result && !seg.error && (
                        <details className="mt-1">
                          <summary className="text-[10px] cursor-pointer" style={{ color: 'var(--color-muted-foreground)' }}>
                            Result
                          </summary>
                          <pre className="text-[10px] mt-1 p-2 rounded overflow-x-auto" style={{
                            backgroundColor: 'var(--color-surface-secondary)',
                            color: 'var(--color-foreground)',
                            maxHeight: '120px',
                          }}>
                            {seg.result}
                          </pre>
                        </details>
                      )}
                      {seg.error && (
                        <pre className="text-[10px] mt-1 p-2 rounded overflow-x-auto" style={{
                          backgroundColor: 'color-mix(in srgb, var(--color-destructive) 8%, transparent)',
                          color: 'var(--color-destructive)',
                          maxHeight: '120px',
                        }}>
                          {seg.error}
                        </pre>
                      )}
                    </div>
                  )
                }
                return null
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
