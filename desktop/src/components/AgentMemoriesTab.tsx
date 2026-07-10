import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Loader2, Info, Award } from 'lucide-react'
import type { MemoryRecord } from '@/types'

// ─── AgentMemoriesTab ──────────────────────────────────────────────────

interface AgentMemoriesTabProps {
  memories: MemoryRecord[] | null
  memoriesLoading: boolean
  memoriesError: string | null
}

const RECORD_TYPE_LABELS: Record<string, string> = {
  observation: 'Observation',
  action: 'Action',
  dialogue: 'Dialogue',
  reflection: 'Reflection',
  plan: 'Plan',
}

export function AgentMemoriesTab({
  memories,
  memoriesLoading,
  memoriesError,
}: AgentMemoriesTabProps) {
  const [memorySearch, setMemorySearch] = useState('')
  const [memoryTypeFilter, setMemoryTypeFilter] = useState('all')

  if (memoriesLoading) {
    return (
      <div className="flex h-32 items-center justify-center text-xs text-muted-foreground font-mono">
        <Loader2 className="mr-2 h-4 w-4 animate-spin text-primary" /> Loading memories...
      </div>
    )
  }
  if (memoriesError) {
    return <div className="text-center text-xs font-mono text-rose-500 py-6">{memoriesError}</div>
  }
  if (!memories || memories.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed border-border/80 rounded-xl bg-card/5">
        <Info className="h-6 w-6 opacity-30" />
        <span className="text-xs">No memory records found.</span>
      </div>
    )
  }

  const filtered = memories
    .filter((m) => {
      const matchSearch =
        m.content.toLowerCase().includes(memorySearch.toLowerCase()) ||
        (m.location && m.location.toLowerCase().includes(memorySearch.toLowerCase()))
      const matchType = memoryTypeFilter === 'all' || m.record_type === memoryTypeFilter
      return matchSearch && matchType
    })
    .reverse() // Show latest memories first

  return (
    <div className="space-y-4">
      {/* Filters */}
      <div className="flex gap-2 shrink-0">
        <input
          type="text"
          placeholder="Search memories..."
          value={memorySearch}
          onChange={(e) => setMemorySearch(e.target.value)}
          className="flex-1 rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:outline-none transition-all"
        />
        <select
          value={memoryTypeFilter}
          onChange={(e) => setMemoryTypeFilter(e.target.value)}
          className="rounded-lg border border-border bg-background px-2 py-1.5 text-xs text-foreground focus:border-primary focus:outline-none transition-all font-mono"
        >
          <option value="all">All Types</option>
          <option value="observation">Observation</option>
          <option value="action">Action</option>
          <option value="dialogue">Dialogue</option>
          <option value="reflection">Reflection</option>
          <option value="plan">Plan</option>
        </select>
      </div>

      <div className="space-y-3">
        {filtered.length === 0 ? (
          <div className="text-center text-xs font-mono text-muted-foreground py-6">
            No memories matching current filter criteria.
          </div>
        ) : (
          filtered.map((m, idx) => {
            const importanceColor =
              m.importance && m.importance >= 7
                ? 'bg-amber-500/10 text-amber-500 border-amber-500/25'
                : m.importance && m.importance >= 4
                  ? 'bg-primary/10 text-primary border-primary/25'
                  : 'bg-muted text-muted-foreground'

            const timeStr = m.simulated_time
              ? new Date(m.simulated_time).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                  hour12: false,
                })
              : ''

            return (
              <div
                key={idx}
                className="rounded-xl border border-border bg-card/20 p-4 space-y-2 text-xs hover:border-primary/30 transition-colors"
              >
                <div className="flex flex-wrap items-center justify-between gap-1.5 border-b border-border/30 pb-1.5 text-[9px] font-mono text-muted-foreground">
                  <div className="flex items-center gap-1.5">
                    <span className="rounded bg-muted px-1.5 py-0.5 text-foreground font-semibold">
                      R{m.round}
                    </span>
                    {m.record_type && (
                      <span className="rounded bg-primary/10 border border-primary/25 text-primary font-bold px-1.5 py-0.5 uppercase tracking-wide">
                        {RECORD_TYPE_LABELS[m.record_type] || m.record_type}
                      </span>
                    )}
                    {timeStr && <span>🕒 {timeStr}</span>}
                    {m.location && <span>📍 {m.location}</span>}
                  </div>
                  {m.importance && (
                    <span
                      className={`px-1.5 py-0.5 rounded border font-semibold ${importanceColor}`}
                    >
                      Importance: {m.importance.toFixed(1)}
                    </span>
                  )}
                </div>
                <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 select-text font-sans leading-relaxed">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.content}</ReactMarkdown>
                </div>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

// ─── AgentReflectionsTab ───────────────────────────────────────────────

interface AgentReflectionsTabProps {
  reflections: MemoryRecord[] | null
  reflectionsLoading: boolean
  reflectionsError: string | null
}

export function AgentReflectionsTab({
  reflections,
  reflectionsLoading,
  reflectionsError,
}: AgentReflectionsTabProps) {
  if (reflectionsLoading) {
    return (
      <div className="flex h-32 items-center justify-center text-xs text-muted-foreground font-mono">
        <Loader2 className="mr-2 h-4 w-4 animate-spin text-primary" /> Loading higher-order reflections...
      </div>
    )
  }
  if (reflectionsError) {
    return (
      <div className="text-center text-xs font-mono text-rose-500 py-6">{reflectionsError}</div>
    )
  }
  if (!reflections || reflections.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed border-border/80 rounded-xl bg-card/5">
        <Info className="h-6 w-6 opacity-30" />
        <span className="text-xs">No reflections generated yet. Reflections are periodically triggered during simulation runtime.</span>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
        <Award className="h-3.5 w-3.5" />
        Agent Reflections & Insights
      </h4>
      <div className="space-y-3">
        {reflections.map((r, idx) => {
          const timeStr = r.simulated_time
            ? new Date(r.simulated_time).toLocaleTimeString([], {
                hour: '2-digit',
                minute: '2-digit',
                hour12: false,
              })
            : ''
          return (
            <div
              key={idx}
              className="rounded-xl border border-border/50 bg-card/30 p-4 space-y-2 text-xs"
            >
              <div className="flex items-center justify-between text-[9px] font-mono text-muted-foreground border-b border-border/20 pb-1 mt-0.5">
                <span>
                  Round {r.round} {timeStr && `• 🕒 ${timeStr}`}
                </span>
                {r.importance && (
                  <span className="bg-amber-500/10 text-amber-500 font-bold px-1.5 py-0.2 rounded border border-amber-500/20">
                    Importance: {r.importance.toFixed(1)}
                  </span>
                )}
              </div>
              <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 select-text italic leading-relaxed">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{r.content}</ReactMarkdown>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
