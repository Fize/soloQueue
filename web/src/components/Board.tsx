import { useState } from 'react';
import type { Plan, PlanStatus } from '@/types';
import { usePlans } from '@/hooks/usePlans';
import { BoardColumn } from './BoardColumn';
import { PlanDetail } from './PlanDetail';
import { Header } from './Header';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  DragOverlay,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from '@dnd-kit/core';
import { PlanCard } from './PlanCard';
import { arrayMove } from '@dnd-kit/sortable';
import { AlertTriangle, RefreshCw } from 'lucide-react';

export function Board() {
  const { plansByStatus, loading, error, fetchPlans, movePlan, plans } = usePlans();
  const [selectedPlan, setSelectedPlan] = useState<Plan | null>(null);
  const [activePlan, setActivePlan] = useState<Plan | null>(null);
  const [activeTab, setActiveTab] = useState<string>('plan');

  // Local state for optimistic reordering during drag
  const [localPlans, setLocalPlans] = useState<Record<PlanStatus, Plan[]> | null>(null);
  const displayPlans = localPlans ?? plansByStatus;

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  function handleDragStart(event: DragStartEvent) {
    const id = event.active.id as string;
    const plan = plans.find((p) => p.id === id);
    setActivePlan(plan ?? null);
  }

  function handleDragOver(event: DragOverEvent) {
    const { active, over } = event;
    if (!over) return;

    const activeId = active.id as string;
    const overId = over.id as string;

    const overStatus = (['plan', 'running', 'done'] as PlanStatus[]).find(
      (s) => s === overId,
    );

    let sourceStatus: PlanStatus | undefined;
    for (const status of ['plan', 'running', 'done'] as PlanStatus[]) {
      if (displayPlans[status].some((p) => p.id === activeId)) {
        sourceStatus = status;
        break;
      }
    }
    if (!sourceStatus) return;

    const destStatus = overStatus ?? sourceStatus;

    if (sourceStatus === destStatus) {
      const items = [...displayPlans[sourceStatus]];
      const oldIndex = items.findIndex((p) => p.id === activeId);
      const newIndex = overStatus
        ? items.length
        : items.findIndex((p) => p.id === overId);
      if (oldIndex === newIndex || oldIndex === -1) return;

      setLocalPlans({
        ...displayPlans,
        [sourceStatus]: arrayMove(items, oldIndex, newIndex),
      });
    } else {
      const sourceItems = displayPlans[sourceStatus].filter((p) => p.id !== activeId);
      const plan = displayPlans[sourceStatus].find((p) => p.id === activeId);
      if (!plan) return;

      const destItems = [...displayPlans[destStatus]];
      const overIndex = overStatus
        ? destItems.length
        : destItems.findIndex((p) => p.id === overId);
      destItems.splice(overIndex === -1 ? destItems.length : overIndex, 0, {
        ...plan,
        status: destStatus,
      });

      setLocalPlans({
        ...displayPlans,
        [sourceStatus]: sourceItems,
        [destStatus]: destItems,
      });
    }
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActivePlan(null);
    setLocalPlans(null);

    if (!over) return;

    const activeId = active.id as string;
    const overId = over.id as string;

    const overStatus = (['plan', 'running', 'done'] as PlanStatus[]).find(
      (s) => s === overId,
    );

    let sourceStatus: PlanStatus | undefined;
    for (const status of ['plan', 'running', 'done'] as PlanStatus[]) {
      if (plansByStatus[status].some((p) => p.id === activeId)) {
        sourceStatus = status;
        break;
      }
    }
    if (!sourceStatus) return;

    const destStatus = overStatus ?? sourceStatus;
    if (sourceStatus !== destStatus) {
      movePlan(activeId, destStatus);
    }
  }

  // Error state
  if (error) {
    return (
      <div className="flex h-screen flex-col bg-background">
        <Header onRefresh={fetchPlans} loading={loading} />
        <div className="flex flex-1 flex-col items-center justify-center gap-4">
          <AlertTriangle className="h-10 w-10 text-status-running" />
          <p className="text-sm text-muted-foreground">{error}</p>
          <Button variant="outline" size="sm" onClick={fetchPlans}>
            <RefreshCw className="mr-2 h-3.5 w-3.5" />
            Retry
          </Button>
        </div>
      </div>
    );
  }

  function handlePlanClick(plan: Plan) {
    setSelectedPlan(plan);
  }

  return (
    <div className="flex h-screen flex-col bg-background">
      <Header onRefresh={fetchPlans} loading={loading} />

      {/* Mobile Tabs */}
      <div className="md:hidden">
        <Tabs value={activeTab} onValueChange={setActiveTab}>
          <div className="border-b border-border px-4">
            <TabsList className="h-10 w-full justify-start gap-1 bg-transparent">
              {(['plan', 'running', 'done'] as PlanStatus[]).map((status) => (
                <TabsTrigger
                  key={status}
                  value={status}
                  className="rounded-md px-3 text-xs capitalize data-[state=active]:bg-primary/10 data-[state=active]:text-primary"
                >
                  {status} ({displayPlans[status].length})
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
        </Tabs>
      </div>

      {/* Board */}
      <DndContext
        sensors={sensors}
        onDragStart={handleDragStart}
        onDragOver={handleDragOver}
        onDragEnd={handleDragEnd}
      >
        {/* Desktop: 3-column layout, centered */}
        <div className="hidden flex-1 overflow-x-auto md:flex">
          <div className="mx-auto flex h-full gap-4 p-6 lg:gap-5 xl:max-w-[1200px] lg:max-w-[1000px]">
            {(['plan', 'running', 'done'] as PlanStatus[]).map((status) => (
              <div key={status} className="w-[320px] shrink-0 lg:w-[340px]">
                <BoardColumn
                  status={status}
                  plans={displayPlans[status]}
                  onPlanClick={handlePlanClick}
                />
              </div>
            ))}
          </div>
        </div>

        {/* Mobile: single column based on active tab, centered */}
        <div className="flex-1 overflow-y-auto p-4 md:hidden">
          <div className="mx-auto max-w-[400px]">
            <BoardColumn
              status={activeTab as PlanStatus}
              plans={displayPlans[activeTab as PlanStatus]}
              onPlanClick={handlePlanClick}
            />
          </div>
        </div>

        <DragOverlay dropAnimation={{ duration: 200, easing: 'cubic-bezier(0.18, 0.67, 0.83, 0.67)' }}>
          {activePlan && <PlanCard plan={activePlan} onClick={() => {}} isOverlay />}
        </DragOverlay>
      </DndContext>

      {/* Plan detail sheet */}
      {selectedPlan && (
        <PlanDetail
          plan={selectedPlan}
          open={true}
          onClose={() => setSelectedPlan(null)}
        />
      )}
    </div>
  );
}
