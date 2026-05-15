import type { Plan } from '@/types';
import { cn } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import { Clock, CheckCircle2, Circle } from 'lucide-react';

interface PlanCardProps {
  plan: Plan;
  onClick: () => void;
  /** True when rendered inside DragOverlay */
  isOverlay?: boolean;
  /** True when this card is being dragged (original position) */
  isDragging?: boolean;
}

const statusIcon = {
  plan: Circle,
  running: Clock,
  done: CheckCircle2,
};

const statusColor = {
  plan: 'text-[#635BFF]',
  running: 'text-[#FFB020]',
  done: 'text-[#00D924]',
};

export function PlanCard({ plan, onClick, isOverlay, isDragging }: PlanCardProps) {
  const Icon = statusIcon[plan.status];
  const totalTodos = plan.todo_items?.length ?? 0;
  const completedTodos = plan.todo_items?.filter((t) => t.completed).length ?? 0;
  const progress = totalTodos > 0 ? (completedTodos / totalTodos) * 100 : 0;

  const tags = plan.tags
    ? plan.tags.split(',').map((t) => t.trim()).filter(Boolean)
    : [];

  return (
    <div
      className={cn(
        'group relative cursor-pointer rounded-lg border bg-card p-3.5 shadow-sm transition-all duration-200 select-none hover:shadow-md hover:-translate-y-0.5',
        // Original being dragged → dim it
        isDragging && 'opacity-30 scale-[0.98]',
        // Overlay ghost → clean floating look
        isOverlay && 'shadow-lg border-primary scale-105',
      )}
      onClick={onClick}
    >
      {/* Status icon + title */}
      <div className="mb-2 flex items-start gap-2">
        <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', statusColor[plan.status])} />
        <h3 className="text-sm font-medium leading-snug text-card-foreground line-clamp-2">
          {plan.title}
        </h3>
      </div>

      {/* Tags */}
      {tags.length > 0 && (
        <div className="mb-2.5 flex flex-wrap gap-1">
          {tags.slice(0, 3).map((tag) => (
            <Badge key={tag} variant="secondary" className="h-5 text-[10px] font-normal px-1.5">
              {tag}
            </Badge>
          ))}
          {tags.length > 3 && (
            <Badge variant="secondary" className="h-5 text-[10px] font-normal px-1.5">
              +{tags.length - 3}
            </Badge>
          )}
        </div>
      )}

      {/* Progress bar */}
      {totalTodos > 0 && (
        <div className="space-y-1">
          <div className="flex items-center justify-between text-[11px] text-muted-foreground">
            <span>{completedTodos}/{totalTodos} tasks</span>
            <span>{Math.round(progress)}%</span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-sm bg-muted">
            <div
              className="h-full rounded-sm bg-[#00D924] transition-all duration-300"
              style={{ width: `${progress}%` }}
            />
          </div>
        </div>
      )}

      {/* Footer */}
      <div className="mt-2.5 flex items-center justify-between text-[11px] text-muted-foreground">
        {plan.creator && <span>@{plan.creator}</span>}
        <span>{new Date(plan.updated_at).toLocaleDateString()}</span>
      </div>
    </div>
  );
}
