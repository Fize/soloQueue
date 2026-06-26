import type { ChatSession, AgentInfo, LLMModel } from '@/types'
import { StatusBadge } from '@/components/ui/status-badge'
import { Cpu, Layers, Lock } from 'lucide-react'
import { useState, useEffect } from 'react'
import { listModels, getDefaultModels } from '@/lib/api'

interface L2SessionStatusPanelProps {
  session: ChatSession
  activeAgent: AgentInfo | null
}

export function L2SessionStatusPanel({ session, activeAgent }: L2SessionStatusPanelProps) {
  const [fastModel, setFastModel] = useState<LLMModel | null>(null)

  // Fetch the live fast model from config on mount (so model changes are reflected immediately).
  useEffect(() => {
    let cancelled = false
    async function fetchFastModel() {
      try {
        const [models, defaults] = await Promise.all([listModels(), getDefaultModels()])
        if (cancelled) return
        // The fast ref is "provider:id" format, e.g. "deepseek:deepseek-v4-flash".
        const fastRef = defaults.fast || defaults.fallback
        if (!fastRef) return
        const colonIdx = fastRef.indexOf(':')
        if (colonIdx === -1) return
        const providerId = fastRef.slice(0, colonIdx)
        const modelId = fastRef.slice(colonIdx + 1)
        const found = models.find((m) => m.providerId === providerId && m.id === modelId)
        if (found) setFastModel(found)
      } catch {
        // Non-critical: context limit will fall back to session value.
      }
    }
    fetchFastModel()
    return () => {
      cancelled = true
    }
  }, [])

  const isProcessing = activeAgent?.state === 'processing'
  const isLocked = activeAgent?.level_locked ?? false

  // Provider: prefer live agent data (has override during processing), fallback to config.
  const providerId = activeAgent?.provider_id || fastModel?.providerId || ''

  // During active processing the agent's model_id reflects the router-assigned override.
  // While idle it falls back to the template definition, so we prefer the live config value.
  const displayModel =
    isProcessing && activeAgent?.model_id
      ? activeAgent.model_id
      : fastModel?.name || fastModel?.id || activeAgent?.model_id

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
        {/* Agent Info Card */}
        <div className="relative group rounded-2xl border border-border/40 bg-card/40 backdrop-blur-md p-5 shadow-lg shadow-black/5 overflow-hidden transition-all duration-300 hover:border-violet-500/20 hover:shadow-violet-500/5">
          {/* Ambient glow */}
          <div className="absolute -right-8 -top-8 w-24 h-24 bg-violet-500/10 rounded-full blur-2xl group-hover:bg-violet-500/15 transition-all duration-300 pointer-events-none" />

          <div className="space-y-4">
            {/* Name / role */}
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

            {/* Model — always live from config, labelled differently during processing */}
            {displayModel && (
              <div className="pt-3 border-t border-border/20 space-y-1">
                <span className="text-[10px] font-semibold text-muted-foreground/60">
                  {isLocked
                    ? 'Locked — Active Model'
                    : isProcessing
                      ? 'Active Model'
                      : 'Default Model (Fast)'}
                </span>
                <div className="font-mono text-[9px] text-foreground bg-muted/40 p-2 rounded-lg border border-border/10 flex items-center gap-1.5 min-w-0">
                  {/* Provider badge */}
                  {providerId && (
                    <span className="shrink-0 text-[8px] font-bold uppercase tracking-wide text-violet-400 bg-violet-500/10 border border-violet-500/20 px-1.5 py-0.5 rounded">
                      {providerId}
                    </span>
                  )}
                  {isLocked && <Lock className="h-3 w-3 text-amber-400 shrink-0" />}
                  <span className="truncate">{displayModel}</span>
                </div>
              </div>
            )}

            {/* Iteration + Task Level — only shown during active processing or locked */}
            {activeAgent &&
              (activeAgent.iteration !== undefined || activeAgent.task_level || isLocked) && (
                <div className="pt-3 border-t border-border/20 flex flex-wrap items-start gap-x-6 gap-y-3">
                  {activeAgent.iteration !== undefined && (
                    <div className="space-y-1">
                      <span className="text-[10px] font-semibold text-muted-foreground/60 block">
                        Iteration
                      </span>
                      <div className="font-mono text-xs font-semibold text-violet-500 bg-violet-500/10 px-2.5 py-1 rounded-lg border border-violet-500/20 w-fit min-w-[50px] text-center">
                        {activeAgent.iteration}
                      </div>
                    </div>
                  )}
                  {(activeAgent.task_level || isLocked) && (
                    <div className="space-y-1 min-w-0">
                      <span className="text-[10px] font-semibold text-muted-foreground/60 block">
                        Task Level
                      </span>
                      <div className="font-mono text-xs font-semibold text-foreground bg-muted/40 px-2.5 py-1 rounded-lg border border-border/10 w-fit max-w-full capitalize flex items-center gap-1.5">
                        {isLocked && <Lock className="h-3 w-3 text-amber-400 shrink-0" />}
                        <span
                          className="truncate"
                          title={activeAgent.task_level || activeAgent.last_level || undefined}
                        >
                          {activeAgent.task_level || activeAgent.last_level || '—'}
                        </span>
                      </div>
                    </div>
                  )}
                </div>
              )}
          </div>
        </div>

      </div>
    </div>
  )
}
