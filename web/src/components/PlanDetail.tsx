import { useState, useEffect } from 'react';
import type { Plan } from '@/types';
import { getPlan, toggleTodo } from '@/lib/api';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { Badge } from '@/components/ui/badge';
import { Progress } from '@/components/ui/progress';
import { TodoList } from './TodoList';
import { Calendar, Tag, User, Loader2, ListChecks } from 'lucide-react';
import { cn } from '@/lib/utils';

interface PlanDetailProps {
  plan: Plan;
  open: boolean;
  onClose: () => void;
}

const statusLabel = {
  plan: 'Plan',
  running: 'Running',
  done: 'Done',
} as const;

const statusBadgeClass = {
  plan: 'bg-status-plan text-foreground border-border',
  running: 'bg-status-running text-foreground border-border',
  done: 'bg-status-done text-foreground border-border',
};

export function PlanDetail({ plan, open, onClose }: PlanDetailProps) {
  const [fullPlan, setFullPlan] = useState<Plan | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    getPlan(plan.id)
      .then((data) => setFullPlan(data))
      .catch(() => setFullPlan(plan))
      .finally(() => setLoading(false));
  }, [open, plan.id, plan]);

  const tags = fullPlan?.tags
    ? fullPlan.tags.split(',').map((t) => t.trim()).filter(Boolean)
    : [];

  const todos = fullPlan?.todo_items ?? [];
  const completedCount = todos.filter((t) => t.completed).length;
  const progressPct = todos.length > 0 ? Math.round((completedCount / todos.length) * 100) : 0;

  function handleToggleTodo(todoId: string) {
    if (!fullPlan) return;
    toggleTodo(fullPlan.id, todoId)
      .then((updated) => {
        setFullPlan((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            todo_items:
              prev.todo_items?.map((t) => (t.id === todoId ? updated : t)) ?? [],
          };
        });
      })
      .catch(() => {});
  }

  function handleTodoDelete(todoId: string) {
    setFullPlan((prev) => {
      if (!prev) return prev;
      return {
        ...prev,
        todo_items: prev.todo_items?.filter((t) => t.id !== todoId) ?? [],
      };
    });
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-xl max-h-[85vh] flex flex-col p-0 overflow-hidden">
        {/* Header */}
        <DialogHeader className="px-6 pt-6 pb-0">
          <DialogTitle className="text-lg font-semibold leading-tight pr-4">
            {fullPlan?.title ?? plan.title}
          </DialogTitle>
        </DialogHeader>

        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <Loader2 className="h-7 w-7 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <>
            <ScrollArea className="flex-1 max-h-[calc(85vh-8rem)] px-6 py-5">
              <div className="space-y-5">
                {/* Status + meta row */}
                <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                  <Badge
                    variant="outline"
                    className={cn('border font-medium', statusBadgeClass[fullPlan?.status ?? plan.status])}
                  >
                    {statusLabel[fullPlan?.status ?? plan.status]}
                  </Badge>

                  {fullPlan?.creator && (
                    <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                      <User className="h-3 w-3" />
                      {fullPlan.creator}
                    </span>
                  )}

                  <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                    <Calendar className="h-3 w-3" />
                    {new Date(fullPlan?.created_at ?? plan.created_at).toLocaleDateString()}
                  </span>
                </div>

                {/* Tags */}
                {tags.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {tags.map((tag) => (
                      <span key={tag} className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                        <Tag className="h-2.5 w-2.5" />
                        {tag}
                      </span>
                    ))}
                  </div>
                )}

                {/* Content */}
                {fullPlan?.content && (
                  <div className="rounded-lg border-2 border-[#EEEEEE] bg-muted/30 p-4">
                    <p className="text-sm leading-relaxed text-muted-foreground whitespace-pre-wrap">
                      {fullPlan.content}
                    </p>
                  </div>
                )}

                <Separator />

                {/* Tasks section */}
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold tracking-tight text-foreground flex items-center gap-2">
                      <ListChecks className="h-4 w-4 text-primary" />
                      Tasks
                    </h3>

                    {todos.length > 0 && (
                      <div className="flex items-center gap-2">
                        <span className="text-xs tabular-nums text-muted-foreground">
                          {completedCount}/{todos.length}
                        </span>
                        <span className="text-xs text-muted-foreground">completed</span>
                        <Progress value={progressPct} className="w-20 h-1.5" />
                      </div>
                    )}
                  </div>

                  <TodoList
                    todos={todos}
                    onToggle={handleToggleTodo}
                    onDelete={handleTodoDelete}
                    planId={fullPlan?.id ?? plan.id}
                  />
                </div>
              </div>
            </ScrollArea>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
