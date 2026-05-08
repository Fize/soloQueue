import type { TodoItemWithDeps } from '@/types';
import { deleteTodo } from '@/lib/api';
import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/lib/utils';
import { Trash2, AlertTriangle, GripVertical, CheckCircle2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';

interface TodoListProps {
  todos: TodoItemWithDeps[];
  onToggle: (todoId: string) => void;
  onDelete: (todoId: string) => void;
  planId: string;
}

export function TodoList({ todos, onToggle, onDelete, planId }: TodoListProps) {
  if (todos.length === 0) {
    return (
      <div className="flex h-14 items-center justify-center rounded-lg border border-dashed border-border/60 text-xs text-muted-foreground/70">
        No tasks yet
      </div>
    );
  }

  return (
    <div className="space-y-0.5">
      {todos.map((todo) => (
        <TodoItemRow
          key={todo.id}
          todo={todo}
          onToggle={onToggle}
          onDelete={onDelete}
          planId={planId}
        />
      ))}
    </div>
  );
}

function TodoItemRow({
  todo,
  onToggle,
  onDelete,
  planId,
}: {
  todo: TodoItemWithDeps;
  onToggle: (todoId: string) => void;
  onDelete: (todoId: string) => void;
  planId: string;
}) {
  const deps = todo.depends_on ?? [];
  const hasDeps = deps.length > 0;
  const blocked = hasDeps && !todo.completed;

  function handleDelete() {
    deleteTodo(planId, todo.id)
      .then(() => onDelete(todo.id))
      .catch(() => {});
  }

  return (
    <div
      className={cn(
        'group relative flex items-start gap-2.5 rounded-lg px-3 py-2.5 transition-colors',
        'hover:bg-accent/40',
        todo.completed && 'opacity-55',
      )}
    >
      {/* Drag grip */}
      <GripVertical className="mt-0.5 h-4 w-4 shrink-0 cursor-grab text-transparent group-hover:text-muted-foreground/30" />

      <Checkbox
        checked={todo.completed}
        onCheckedChange={() => onToggle(todo.id)}
        disabled={blocked && !todo.completed}
        className={cn(
          'mt-0.5 h-4 w-4 shrink-0 rounded-sm border-input',
          todo.completed && 'text-status-done data-[state=checked]:bg-status-done/20 data-[state=checked]:border-status-done/50',
        )}
      />

      <div className="min-w-0 flex-1 pt-px">
        <span
          className={cn(
            'text-[13px] leading-relaxed',
            todo.completed && 'line-through text-muted-foreground',
          )}
        >
          {todo.content}
        </span>
      </div>

      {/* Right actions */}
      <div className="flex shrink-0 items-center gap-1 pt-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
        {blocked && !todo.completed && (
          <Tooltip>
            <TooltipTrigger className="inline-flex">
              <AlertTriangle className="h-3.5 w-3.5 text-status-running" />
            </TooltipTrigger>
            <TooltipContent side="top" className="text-xs">
              Blocked by unfinished dependencies
            </TooltipContent>
          </Tooltip>
        )}

        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleDelete}
          className="h-6 w-6 text-muted-foreground hover:text-destructive"
        >
          <Trash2 className="h-3 w-3" />
        </Button>
      </div>

      {/* Completed indicator */}
      {todo.completed && (
        <CheckCircle2 className="mt-0.5 absolute right-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-status-done opacity-0 group-hover:opacity-100 transition-opacity" />
      )}
    </div>
  );
}
