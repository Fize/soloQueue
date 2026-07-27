import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { AlertTriangle, Bot, Link2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { GraphNode } from '@/types'

// ─── Custom ReactFlow Node ──────────────────────────────────────────────

function WorkflowNodeComponent({ data, selected }: NodeProps) {
  const nodeData = data as unknown as GraphNode & {
    isEntry?: boolean
    isTerminal?: boolean
    outcomeKeys?: string[]
    agentTemplate?: string
    agentDisplayName?: string
    invalidAgent?: boolean
    isConnecting?: boolean
    pendingConnection?: { source: string; outcome: string } | null
    onStartConnection?: (source: string, outcome: string) => void
    onCompleteConnection?: (target: string) => void
    connectLabel?: string
    connectTargetLabel?: string
  }
  const isEntry = nodeData.isEntry || false
  const isTerminal = nodeData.isTerminal || false
  const outcomeKeys = nodeData.outcomeKeys || Object.keys(nodeData.outputs || {})

  return (
    <div
      className={cn(
        'w-[300px] rounded-xl border bg-card shadow-sm transition-colors',
        isEntry && 'border-accent/50 bg-accent/5',
        nodeData.invalidAgent && 'border-rose-500/60 bg-rose-500/5',
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
        className={cn(
          '!h-4 !w-4 !rounded-full !border-[3px] !border-card !bg-muted-foreground/50 transition-all',
          nodeData.isConnecting && '!scale-125 !bg-success !shadow-[0_0_0_5px_rgba(34,197,94,0.16)]'
        )}
        title="Connect to this node"
      />

      {/* Node content */}
      <div className="px-3 py-2.5">
        {nodeData.pendingConnection && (
          <button
            type="button"
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => {
              event.stopPropagation()
              nodeData.onCompleteConnection?.(nodeData.id)
            }}
            className="mb-2 flex w-full items-center justify-center gap-1.5 rounded-md border border-success/30 bg-success/10 px-2 py-1.5 text-[9px] font-semibold text-success hover:bg-success/15"
          >
            <Link2 className="h-3 w-3" />
            {nodeData.connectTargetLabel}
          </button>
        )}
        <div className="text-[9px] font-bold uppercase tracking-wider text-muted-foreground">
          NODE · nodes[].id
        </div>
        <div className="mt-0.5 truncate font-mono text-sm font-bold text-foreground">
          {nodeData.id}
        </div>
        <div className="mt-2 flex items-center gap-1.5 rounded-md bg-muted/35 px-2 py-1.5">
          {nodeData.invalidAgent ? (
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-rose-500" />
          ) : (
            <Bot className="h-3.5 w-3.5 shrink-0 text-primary" />
          )}
          <span className={cn(
            'min-w-0 flex-1 truncate text-[11px] font-medium',
            nodeData.invalidAgent ? 'text-rose-500' : 'text-foreground'
          )}>
            {nodeData.agentDisplayName || nodeData.agentTemplate || 'Unknown agent'}
          </span>
          <span className="truncate font-mono text-[8px] text-muted-foreground">
            agent: {nodeData.agent || '—'}
          </span>
        </div>
      </div>

      {/* Output handles — one per outcome, stacked vertically on the right */}
      <div className="relative border-t border-border/30 px-3 py-1.5">
        {outcomeKeys.length > 0 ? (
          outcomeKeys.map((outcome) => (
            <div key={outcome} className="relative flex items-center justify-between gap-2 py-1">
              <span className="font-mono text-[9px] text-muted-foreground">
                outputs.{outcome}.to
              </span>
              <button
                type="button"
                onPointerDown={(event) => event.stopPropagation()}
                onClick={(event) => {
                  event.stopPropagation()
                  nodeData.onStartConnection?.(nodeData.id, outcome)
                }}
                className="flex items-center gap-1 rounded px-1.5 py-1 font-mono text-[9px] font-semibold text-primary hover:bg-primary/10"
                title={nodeData.connectLabel}
              >
                <Link2 className="h-3 w-3" />
                {outcome} · {nodeData.connectLabel}
              </button>
              <Handle
                type="source"
                position={Position.Right}
                id={outcome}
                className="!-right-[18px] !h-4 !w-4 !cursor-crosshair !rounded-full !border-[3px] !border-card !bg-chart-1 hover:!scale-125 hover:!shadow-[0_0_0_5px_rgba(59,130,246,0.16)]"
                title={`Drag to connect outcome "${outcome}"`}
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
