import { useState, useRef, useMemo, useEffect } from 'react'
import type { Plan, PlanStatus } from '@/types'
import { usePlanStore } from '@/stores/planStore'
import { BoardColumn } from './BoardColumn'
import { PlanDetail } from './PlanDetail'
import { PlanCreateDialog } from './PlanCreateDialog'
import { Button } from '@/components/ui/button'
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  DragOverlay,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from '@dnd-kit/core'
import { PlanCard } from './PlanCard'
import { arrayMove } from '@dnd-kit/sortable'
import { AlertTriangle, RefreshCw, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'

const mobileColumnConfig: Record<PlanStatus, { label: string; dot: string }> = {
  plan: { label: 'Plan', dot: 'bg-[#635BFF]' },
  running: { label: 'Running', dot: 'bg-[#FFB020]' },
  done: { label: 'Done', dot: 'bg-[#00D924]' },
}

export function Board() {
  const plans = usePlanStore((state) => state.plans)
  const error = usePlanStore((state) => state.error)
  const movePlan = usePlanStore((state) => state.movePlan)
  const fetchPlans = usePlanStore((state) => state.fetchPlans)

  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [activeStatus, setActiveStatus] = useState<PlanStatus>('plan')

  const plansByStatus = useMemo(
    () => ({
      plan: plans.filter((p) => p.status === 'plan'),
      running: plans.filter((p) => p.status === 'running'),
      done: plans.filter((p) => p.status === 'done'),
    }),
    [plans]
  )

  const [selectedPlan, setSelectedPlan] = useState<Plan | null>(null)
  const [activePlan, setActivePlan] = useState<Plan | null>(null)

  // Local state for optimistic reordering during drag
  const [localPlans, setLocalPlans] = useState<Record<PlanStatus, Plan[]> | null>(null)
  const displayPlans = localPlans ?? plansByStatus

  // Track last valid over target so drops in gaps still land correctly
  const lastOverRef = useRef<{ id: string; status: PlanStatus } | null>(null)

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  useEffect(() => {
    fetchPlans()
  }, [fetchPlans])

  function findPlanStatus(id: string, source: Record<PlanStatus, Plan[]>): PlanStatus | undefined {
    return (['plan', 'running', 'done'] as PlanStatus[]).find((s) =>
      source[s].some((p) => p.id === id)
    )
  }

  function resolveOverStatus(
    overId: string,
    data: Record<string, unknown> | undefined,
    plansMap: Record<PlanStatus, Plan[]>
  ): PlanStatus | undefined {
    // Direct column droppable match
    if ((['plan', 'running', 'done'] as PlanStatus[]).includes(overId as PlanStatus)) {
      return overId as PlanStatus
    }
    // Sortable item data (most reliable for cross-column)
    const dataStatus = data?.status as PlanStatus | undefined
    if (dataStatus && (['plan', 'running', 'done'] as PlanStatus[]).includes(dataStatus)) {
      return dataStatus
    }
    // Fallback: lookup in current plan map
    return findPlanStatus(overId, plansMap)
  }

  function handleDragStart(event: DragStartEvent) {
    const id = event.active.id as string
    const plan = plans.find((p) => p.id === id)
    setActivePlan(plan ?? null)
    lastOverRef.current = null
  }

  function handleDragOver(event: DragOverEvent) {
    const { active, over } = event
    if (!over) return

    const activeId = active.id as string
    const overId = over.id as string

    const sourceStatus = findPlanStatus(activeId, displayPlans)
    if (!sourceStatus) return

    const destStatus = resolveOverStatus(overId, over.data.current, displayPlans) ?? sourceStatus

    // Record last valid over target
    lastOverRef.current = { id: overId, status: destStatus }

    if (sourceStatus === destStatus) {
      // Same column: reorder
      const items = [...displayPlans[sourceStatus]]
      const oldIndex = items.findIndex((p) => p.id === activeId)
      const isOverColumn = (['plan', 'running', 'done'] as PlanStatus[]).includes(
        overId as PlanStatus
      )
      const newIndex = isOverColumn ? items.length : items.findIndex((p) => p.id === overId)
      if (oldIndex === newIndex || oldIndex === -1) return

      setLocalPlans({
        ...displayPlans,
        [sourceStatus]: arrayMove(items, oldIndex, newIndex),
      })
    } else {
      // Cross column: move item
      const sourceItems = displayPlans[sourceStatus].filter((p) => p.id !== activeId)
      const plan = displayPlans[sourceStatus].find((p) => p.id === activeId)
      if (!plan) return

      const destItems = [...displayPlans[destStatus]]
      const isOverColumn = (['plan', 'running', 'done'] as PlanStatus[]).includes(
        overId as PlanStatus
      )
      const overIndex = isOverColumn
        ? destItems.length
        : destItems.findIndex((p) => p.id === overId)
      destItems.splice(overIndex === -1 ? destItems.length : overIndex, 0, {
        ...plan,
        status: destStatus,
      })

      setLocalPlans({
        ...displayPlans,
        [sourceStatus]: sourceItems,
        [destStatus]: destItems,
      })
    }
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event
    setActivePlan(null)
    setLocalPlans(null)

    const activeId = active.id as string

    // Resolve destination: use event.over if present, fallback to last valid over
    const destStatus = over
      ? resolveOverStatus(over.id as string, over.data.current, plansByStatus)
      : (lastOverRef.current?.status as PlanStatus | undefined)

    const sourceStatus = findPlanStatus(activeId, plansByStatus)
    lastOverRef.current = null

    if (sourceStatus && destStatus && sourceStatus !== destStatus) {
      movePlan(activeId, destStatus)
    }
  }

  // Error state
  if (error) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4">
        <AlertTriangle className="h-10 w-10 text-status-running" />
        <p className="text-sm text-muted-foreground">{error}</p>
        <Button variant="outline" size="sm" onClick={fetchPlans}>
          <RefreshCw className="mr-2 h-3.5 w-3.5" />
          Retry
        </Button>
      </div>
    )
  }

  function handlePlanClick(plan: Plan) {
    setSelectedPlan(plan)
  }

  return (
    <div className="flex flex-1 flex-col min-h-0">
      {/* Top bar */}
      <div className="flex items-center justify-between shrink-0 px-1">
        <h2 className="text-sm font-semibold text-foreground">Plans Board</h2>
        <Button
          size="sm"
          className="h-7 gap-1 text-xs md:h-8 md:gap-1.5"
          onClick={() => setShowCreateDialog(true)}
        >
          <Plus className="h-3.5 w-3.5" />
          New Plan
        </Button>
      </div>

      {/* Desktop: DnD Kanban */}
      <div className="hidden md:flex flex-1 min-h-0">
        <DndContext
          sensors={sensors}
          onDragStart={handleDragStart}
          onDragOver={handleDragOver}
          onDragEnd={handleDragEnd}
        >
          <div className="flex-1 min-h-0 flex">
            <div className="flex w-full gap-4 overflow-x-auto py-5 flex-1 min-h-0">
              {(['plan', 'running', 'done'] as PlanStatus[]).map((status) => (
                <div key={status} className="min-w-[250px] flex-1 flex flex-col min-h-0">
                  <BoardColumn
                    status={status}
                    plans={displayPlans[status]}
                    onPlanClick={handlePlanClick}
                  />
                </div>
              ))}
            </div>
          </div>

          <DragOverlay
            dropAnimation={{ duration: 200, easing: 'cubic-bezier(0.18, 0.67, 0.83, 0.67)' }}
          >
            {activePlan && <PlanCard plan={activePlan} onClick={() => {}} isOverlay />}
          </DragOverlay>
        </DndContext>
      </div>

      {/* Mobile: Tab bar + single column */}
      <div className="flex flex-col flex-1 min-h-0 md:hidden">
        {/* Status tabs */}
        <div className="flex gap-1.5 mb-2 mt-3">
          {(['plan', 'running', 'done'] as PlanStatus[]).map((status) => {
            const cfg = mobileColumnConfig[status]
            return (
              <button
                key={status}
                onClick={() => setActiveStatus(status)}
                className={cn(
                  'flex flex-1 items-center justify-center gap-1.5 rounded-lg px-2 py-2.5 text-xs font-medium transition-colors',
                  activeStatus === status
                    ? 'bg-card border border-border shadow-sm text-foreground'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                <div className={cn('h-2 w-2 rounded-full shrink-0', cfg.dot)} />
                <span>{cfg.label}</span>
                <span className="ml-auto bg-muted px-1.5 py-0.5 rounded text-[10px] font-bold tabular-nums">
                  {displayPlans[status].length}
                </span>
              </button>
            )
          })}
        </div>

        {/* Card list */}
        <div className="flex-1 overflow-y-auto min-h-0 -mx-3 px-3">
          <div className="space-y-2 pb-4">
            {displayPlans[activeStatus].length === 0 ? (
              <div className="flex h-28 items-center justify-center rounded-lg border border-dashed border-border text-sm text-muted-foreground">
                No plans yet
              </div>
            ) : (
              displayPlans[activeStatus].map((plan) => (
                <PlanCard key={plan.id} plan={plan} onClick={() => handlePlanClick(plan)} />
              ))
            )}
          </div>
        </div>
      </div>

      {/* Plan detail sheet */}
      {selectedPlan && (
        <PlanDetail plan={selectedPlan} open={true} onClose={() => setSelectedPlan(null)} />
      )}

      <PlanCreateDialog open={showCreateDialog} onClose={() => setShowCreateDialog(false)} />
    </div>
  )
}
