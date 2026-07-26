import type { MouseEvent } from 'react'
import { Trash2, ArrowRight, GitBranch, Workflow, ArrowRightLeft, User } from 'lucide-react'
import type { WorkflowMeta } from '@/types'
import { useTranslation } from '@/lib/i18n'

// ─── Props ──────────────────────────────────────────────────────────────

interface WorkflowCardProps {
  meta: WorkflowMeta
  nodeCount?: number
  edgeCount?: number
  agentCount?: number
  onClick: () => void
  onDelete: (e: MouseEvent) => void
}

// ─── Component ──────────────────────────────────────────────────────────

export function WorkflowCard({
  meta,
  nodeCount,
  edgeCount,
  agentCount,
  onClick,
  onDelete,
}: WorkflowCardProps) {
  const { t } = useTranslation()
  const isValid = meta.valid

  return (
    <div
      onClick={onClick}
      className={`group relative flex flex-col gap-3 rounded-xl border bg-card/40 hover:bg-card/70 transition-all cursor-pointer px-5 py-4 ${
        isValid
          ? 'border-border hover:border-border/80'
          : 'border-rose-500/20 hover:border-rose-500/30'
      }`}
    >
      {/* Top row: validation + name */}
      <div className="flex items-start gap-3 min-w-0">
        <div className="flex items-center gap-2 mt-0.5 shrink-0">
          {/* Validation dot */}
          <span
            className={`h-2 w-2 shrink-0 rounded-full ${
              isValid ? 'bg-success' : 'bg-rose-500'
            }`}
          />
          <span
            className={`rounded px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wide ${
              isValid
                ? 'bg-success/10 text-success border border-success/25'
                : 'bg-rose-500/10 text-rose-500 border border-rose-500/25'
            }`}
          >
            {isValid ? t('workflow.valid') : t('workflow.invalid')}
          </span>
        </div>

        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold text-foreground group-hover:text-primary transition-colors truncate leading-tight">
            {meta.name}
          </h3>
          {meta.description && (
            <p className="text-[10px] text-muted-foreground truncate mt-0.5">
              {meta.description}
            </p>
          )}
        </div>

        <div className="flex items-center gap-1 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
          <button
            onClick={onDelete}
            className="rounded-lg p-1.5 text-muted-foreground hover:text-rose-500 hover:bg-rose-500/10 transition-colors"
            title={t('workflow.deleteTitle')}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
          <ArrowRight className="h-3.5 w-3.5 text-muted-foreground/50" />
        </div>
      </div>

      {/* Bottom row: meta info */}
      <div className="flex items-center gap-4 text-[10px] text-muted-foreground font-mono">
        {meta.version && (
          <span className="flex items-center gap-1">
            <GitBranch className="h-3 w-3" />
            {t('workflow.version', { version: meta.version })}
          </span>
        )}
        {nodeCount !== undefined && (
          <span className="flex items-center gap-1">
            <Workflow className="h-3 w-3" />
            {t('workflow.nodeCount', { count: nodeCount })}
          </span>
        )}
        {edgeCount !== undefined && (
          <span className="flex items-center gap-1">
            <ArrowRightLeft className="h-3 w-3" />
            {t('workflow.edgeCount', { count: edgeCount })}
          </span>
        )}
        {agentCount !== undefined && (
          <span className="flex items-center gap-1">
            <User className="h-3 w-3" />
            {t('workflow.agents', { count: agentCount })}
          </span>
        )}
      </div>
    </div>
  )
}
