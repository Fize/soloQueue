import type { Plan, PlanStatus } from '@/types';
import { PlanCard } from './PlanCard';
import { cn } from '@/lib/utils';
import { useDroppable } from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy, useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';

const columnConfig: Record<PlanStatus, { label: string; dot: string; bg: string }> = {
  plan: { label: 'Plan', dot: 'bg-blue-500', bg: 'bg-status-plan/10' },
  running: { label: 'Running', dot: 'bg-amber-500', bg: 'bg-status-running/10' },
  done: { label: 'Done', dot: 'bg-green-500', bg: 'bg-status-done/10' },
};

interface BoardColumnProps {
  status: PlanStatus;
  plans: Plan[];
  onPlanClick: (plan: Plan) => void;
}

export function BoardColumn({ status, plans, onPlanClick }: BoardColumnProps) {
  const config = columnConfig[status];
  const { setNodeRef, isOver } = useDroppable({ id: status });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        'flex h-full flex-col nb-border rounded-xl transition-colors duration-200 bg-card',
        isOver && 'border-primary border-[3px] bg-primary/[0.03]',
      )}
    >
      {/* Column header */}
      <div className={cn('flex items-center gap-2 rounded-t-xl px-4 py-3', config.bg)}>
        <div className={cn('h-2.5 w-2.5 rounded-full border-2 border-white shadow-sm', config.dot)} />
        <h2 className="text-sm font-bold text-foreground">{config.label}</h2>
        <span className="ml-auto flex h-5 min-w-[20px] items-center justify-center rounded-md nb-border bg-card px-1.5 text-[11px] font-bold text-foreground">
          {plans.length}
        </span>
      </div>

      {/* Cards area */}
      <div className="flex-1 overflow-y-auto p-3">
        <SortableContext items={plans.map((p) => p.id)} strategy={verticalListSortingStrategy}>
          <div className="flex flex-col gap-2.5">
            {plans.length === 0 && (
              <div className="flex h-24 items-center justify-center rounded-lg border-2 border-dashed border-[#EEEEEE] text-xs text-muted-foreground">
                No plans yet
              </div>
            )}
            {plans.map((plan) => (
              <SortablePlanCard key={plan.id} plan={plan} onClick={() => onPlanClick(plan)} />
            ))}
          </div>
        </SortableContext>
      </div>
    </div>
  );
}

function SortablePlanCard({ plan, onClick }: { plan: Plan; onClick: () => void }) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: plan.id,
    data: { status: plan.status },
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
    >
      <PlanCard
        plan={plan}
        onClick={onClick}
        isDragging={isDragging}
      />
    </div>
  );
}
