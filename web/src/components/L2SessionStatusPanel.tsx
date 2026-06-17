import type { ChatSession, AgentInfo } from '@/types'
import { StatusBadge } from '@/components/ui/status-badge'
import { Folder, Cpu, Layers, Copy, Check } from 'lucide-react'
import { useState } from 'react'

interface L2SessionStatusPanelProps {
  session: ChatSession
  activeAgent: AgentInfo | null
}

export function L2SessionStatusPanel({ session, activeAgent }: L2SessionStatusPanelProps) {
  const used = session.ctxwin_used || 0
  const limit = session.ctxwin_limit
  if (!limit || limit <= 0) {
    throw new Error('Context window limit is not configured or available for this session.')
  }
  const pct = Math.min(100, Math.max(0, (used / limit) * 100))
  const [copied, setCopied] = useState(false)

  const formatNumber = (num: number) => {
    return new Intl.NumberFormat().format(num)
  }

  const handleCopy = () => {
    if (!session.project_path) return
    navigator.clipboard.writeText(session.project_path)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="flex flex-col h-full bg-card/10 text-card-foreground">
      {/* Header */}
      <div className="shrink-0 px-5 py-4.5 border-b border-border/40 bg-card/30 backdrop-blur-md flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Layers className="h-4 w-4 text-violet-500 animate-pulse" />
          <h2 className="text-sm font-semibold tracking-wide text-foreground/90">
            Session Details
          </h2>
        </div>
        {activeAgent && <StatusBadge state={activeAgent.state} size="sm" />}
      </div>

      <div className="flex-1 overflow-y-auto p-5 space-y-5">
        {/* Unified Glassmorphic Card */}
        <div className="relative group rounded-2xl border border-border/40 bg-card/40 backdrop-blur-md p-5 shadow-lg shadow-black/5 overflow-hidden transition-all duration-300 hover:border-violet-500/20 hover:shadow-violet-500/5">
          {/* Subtle Ambient Glow */}
          <div className="absolute -right-8 -top-8 w-24 h-24 bg-violet-500/10 rounded-full blur-2xl group-hover:bg-violet-500/15 transition-all duration-300 pointer-events-none" />

          {/* Leader Section */}
          <div className="space-y-4">
            <div className="flex items-start gap-3">
              <div className="h-9 w-9 rounded-xl bg-violet-500/10 flex items-center justify-center border border-violet-500/20">
                <Cpu className="h-4 w-4 text-violet-500" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-[10px] uppercase font-bold tracking-wider text-muted-foreground/60">
                  {session.type === 'l1' ? 'L1 Orchestrator' : session.group || 'Team Agent'}
                </p>
                <h3 className="text-sm font-bold text-foreground truncate mt-0.5">
                  {session.agent_name || (session.type === 'l1' ? 'L1 Agent' : 'Unnamed Agent')}
                </h3>
              </div>
            </div>

            {activeAgent?.model_id && (
              <div className="pt-3 border-t border-border/20 space-y-1">
                <span className="text-[10px] font-semibold text-muted-foreground/60">
                  Effective Model
                </span>
                <div className="font-mono text-[9px] text-foreground bg-muted/40 p-2 rounded-lg border border-border/10 truncate">
                  {activeAgent.model_id}
                </div>
              </div>
            )}

            {activeAgent && (activeAgent.iteration !== undefined || activeAgent.task_level) && (
              <div className="pt-3 border-t border-border/20 grid grid-cols-2 gap-4">
                {activeAgent.iteration !== undefined && (
                  <div className="space-y-1">
                    <span className="text-[10px] font-semibold text-muted-foreground/60">
                      Iteration
                    </span>
                    <div className="font-mono text-xs font-semibold text-violet-500 bg-violet-500/10 px-2.5 py-1 rounded-lg border border-violet-500/20 w-fit min-w-[50px] text-center">
                      {activeAgent.iteration}
                    </div>
                  </div>
                )}
                {activeAgent.task_level && (
                  <div className="space-y-1">
                    <span className="text-[10px] font-semibold text-muted-foreground/60">
                      Task Level
                    </span>
                    <div className="font-mono text-xs font-semibold text-foreground bg-muted/40 px-2.5 py-1 rounded-lg border border-border/10 w-fit capitalize">
                      {activeAgent.task_level}
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Context Usage Card */}
        <div className="relative group rounded-2xl border border-border/40 bg-card/40 backdrop-blur-md p-5 shadow-lg shadow-black/5 overflow-hidden transition-all duration-300 hover:border-violet-500/20 hover:shadow-violet-500/5">
          <div className="absolute -left-8 -bottom-8 w-24 h-24 bg-violet-500/5 rounded-full blur-2xl pointer-events-none" />

          <div className="space-y-4.5">
            <div className="flex items-center justify-between">
              <span className="text-[10px] uppercase font-bold tracking-wider text-muted-foreground/60">
                Context Window
              </span>
              <span className="text-[10px] font-mono font-bold bg-violet-500/10 text-violet-500 px-2 py-0.5 rounded-full">
                {pct.toFixed(1)}% Used
              </span>
            </div>

            <div className="space-y-2">
              <div className="relative h-2 w-full bg-muted/50 rounded-full overflow-hidden border border-border/5">
                <div
                  className="h-full bg-gradient-to-r from-violet-500 to-indigo-500 rounded-full transition-all duration-500 ease-out"
                  style={{ width: `${pct}%` }}
                />
              </div>
              <div className="flex justify-between items-baseline text-xs">
                <span className="text-muted-foreground">Tokens:</span>
                <span className="font-semibold font-mono text-foreground">
                  {formatNumber(used)}{' '}
                  <span className="text-muted-foreground/40 font-normal">/</span>{' '}
                  {formatNumber(limit)}
                </span>
              </div>
            </div>
          </div>
        </div>

        {/* Workspace Card */}
        {session.project_path && (
          <div className="relative group rounded-2xl border border-border/40 bg-card/40 backdrop-blur-md p-5 shadow-lg shadow-black/5 overflow-hidden transition-all duration-300 hover:border-violet-500/20 hover:shadow-violet-500/5">
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-[10px] uppercase font-bold tracking-wider text-muted-foreground/60 flex items-center gap-1.5">
                  <Folder className="h-3.5 w-3.5 text-violet-500" />
                  Workspace
                </span>
                <button
                  onClick={handleCopy}
                  className="text-muted-foreground/50 hover:text-foreground p-1 hover:bg-muted/50 rounded transition-all cursor-pointer"
                  title="Copy path"
                >
                  {copied ? (
                    <Check className="h-3 w-3 text-emerald-500" />
                  ) : (
                    <Copy className="h-3 w-3" />
                  )}
                </button>
              </div>
              <p
                className="text-[10px] font-mono text-muted-foreground break-all bg-muted/20 p-2.5 rounded-lg border border-border/10 leading-normal"
                title={session.project_path}
              >
                {session.project_path}
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
