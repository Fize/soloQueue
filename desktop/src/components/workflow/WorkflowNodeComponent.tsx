import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { cn } from '@/lib/utils'
import type { GraphNode } from '@/types'

// ─── Custom ReactFlow Node ──────────────────────────────────────────────

function WorkflowNodeComponent({ data, selected }: NodeProps) {
  const nodeData = data as unknown as GraphNode & { isEntry?: boolean; isTerminal?: boolean; outcomeKeys?: string[] }
  const isEntry = nodeData.isEntry || false
  const isTerminal = nodeData.isTerminal || false
  const outcomeKeys = nodeData.outcomeKeys || Object.keys(nodeData.outputs || {})

  return (
    <div
      className={cn(
        'rounded-xl border bg-card shadow-sm min-w-[150px] transition-colors',
        isEntry && 'border-accent/50 bg-accent/5',
        isTerminal && 'ring-1 ring-muted-foreground/20',
        !isEntry && !isTerminal && 'border-border/60',
        selected && 'border-primary ring-2 ring-primary/15'
      )}
    >
      {/* Entry badge */}
      {isEntry && (
        <div className="absolute -top-2 left-2 rounded bg-accent px-1.5 py-0.5 text-[7px] font-bold text-accent-foreground font-mono uppercase tracking-wider z-10">
          ENTRY
        </div>
      )}

      {/* Input handle — always present (loop edges can target any node) */}
      <Handle
        type="target"
        position={Position.Left}
        className="!w-2.5 !h-2.5 !bg-muted-foreground/40 !border-2 !border-card !rounded-full"
      />

      {/* Node content */}
      <div className="px-3 py-2.5">
        <div className="text-xs font-bold font-mono text-foreground truncate">
          {nodeData.id}
        </div>
        <div className="text-[10px] text-muted-foreground mt-0.5 truncate">
          {nodeData.agent}
        </div>
      </div>

      {/* Output handles — one per outcome, stacked vertically on the right */}
      <div className="relative border-t border-border/30 px-3 py-1.5">
        {outcomeKeys.length > 0 ? (
          outcomeKeys.map((outcome) => (
            <div key={outcome} className="flex items-center justify-end gap-1.5 py-0.5">
              <span className="text-[8px] text-muted-foreground font-mono">{outcome}</span>
              <Handle
                type="source"
                position={Position.Right}
                id={outcome}
                className="!w-2.5 !h-2.5 !bg-chart-1 !border-2 !border-card !rounded-full !static !transform-none"
              />
            </div>
          ))
        ) : (
          <Handle
            type="source"
            position={Position.Right}
            id="default"
            className="!w-2.5 !h-2.5 !bg-muted-foreground/40 !border-2 !border-card !rounded-full"
          />
        )}
      </div>
    </div>
  )
}

export const WorkflowNode = memo(WorkflowNodeComponent)
