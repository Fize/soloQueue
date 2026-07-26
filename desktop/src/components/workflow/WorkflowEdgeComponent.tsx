import { memo } from 'react'
import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  type EdgeProps,
} from '@xyflow/react'
import { cn } from '@/lib/utils'

// ─── Custom ReactFlow Edge ──────────────────────────────────────────────

function WorkflowEdgeComponent({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  selected,
  markerEnd,
}: EdgeProps) {
  const loop = (data as any)?.loop as boolean || false
  const outcome = (data as any)?.outcome as string || ''

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    borderRadius: 8,
  })

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        className={cn(
          loop
            ? '!stroke-chart-2'
            : '!stroke-chart-1',
          selected && '!stroke-primary',
        )}
        style={{
          strokeWidth: selected ? 2.5 : 1.5,
          strokeDasharray: loop ? '4,4' : 'none',
        }}
        markerEnd={markerEnd}
      />

      {/* Edge label */}
      {outcome && !loop && (
        <EdgeLabelRenderer>
          <div
            className="absolute pointer-events-none"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            }}
          >
            <span className="rounded bg-card/90 px-1.5 py-0.5 text-[9px] font-mono text-muted-foreground border border-border/60 shadow-sm">
              {outcome}
            </span>
          </div>
        </EdgeLabelRenderer>
      )}

      {loop && (
        <EdgeLabelRenderer>
          <div
            className="absolute pointer-events-none"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY - 12}px)`,
            }}
          >
            <span className="rounded bg-warning/10 px-1.5 py-0.5 text-[9px] font-mono text-warning border border-warning/25 shadow-sm">
              loop
            </span>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

export const WorkflowEdge = memo(WorkflowEdgeComponent)
