import { Trash2 } from 'lucide-react'
import { Input } from '@/components/ui/input'
import type { GraphEdge } from '@/types'
import { useTranslation } from '@/lib/i18n'

// ─── Props ──────────────────────────────────────────────────────────────

interface EdgePropertyPanelProps {
  edge: GraphEdge
  onUpdate: (updates: Partial<GraphEdge>) => void
  onDelete: () => void
}

// ─── Component ──────────────────────────────────────────────────────────

export function EdgePropertyPanel({
  edge,
  onUpdate,
  onDelete,
}: EdgePropertyPanelProps) {
  const { t } = useTranslation()

  return (
    <div className="w-80 shrink-0 border-l border-border/40 bg-card/10 flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="shrink-0 flex items-center justify-between px-4 py-3 border-b border-border/40">
        <div>
          <h3 className="text-xs font-semibold text-foreground font-mono">
            {edge.source} → {edge.target}
          </h3>
        </div>
        <button
          type="button"
          onClick={onDelete}
          className="rounded-lg p-1 text-muted-foreground hover:text-rose-500 hover:bg-rose-500/10 transition-colors"
          title={t('common.delete')}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {/* Outcome */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
            Outcome
          </label>
          <Input
            value={edge.outcome}
            onChange={(e) => onUpdate({ outcome: e.target.value })}
            className="font-mono text-xs"
            placeholder={t('workflow.outcomePlaceholder')}
          />
        </div>

        {/* Loop toggle */}
        <div>
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              checked={edge.loop}
              onChange={(e) => onUpdate({ loop: e.target.checked })}
              className="h-3 w-3 rounded accent-primary"
            />
            <span className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
              {t('workflow.loop')}
            </span>
          </label>
        </div>

        {/* Max traversals (if loop) */}
        {edge.loop && (
          <div>
            <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
              {t('workflow.maxTraversals')}
            </label>
            <Input
              type="number"
              value={edge.maxTraversals || 1}
              onChange={(e) => onUpdate({ maxTraversals: parseInt(e.target.value) || 1 })}
              className="font-mono text-xs w-20"
              min={1}
            />
          </div>
        )}

        {/* Divider */}
        <div className="border-t border-border/40" />

        {/* Info */}
        <div className="text-[10px] text-muted-foreground font-mono space-y-1">
          <div>
            <span className="text-muted-foreground/60">Source:</span> {edge.source}
          </div>
          <div>
            <span className="text-muted-foreground/60">Target:</span> {edge.target}
          </div>
        </div>
      </div>
    </div>
  )
}
