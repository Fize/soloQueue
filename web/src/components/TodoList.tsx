import { useState } from 'react'
import type { TodoItemWithDeps } from '@/types'
import { cn } from '@/lib/utils'
import { Trash2, AlertTriangle, GripVertical, CheckCircle2, GitBranch, Pencil } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Checkbox } from '@/components/ui/checkbox'
import { DependencyChain } from './DependencyChain'
import { DependencyEditDialog } from './DependencyEditDialog'

interface TodoListProps {
  todos: TodoItemWithDeps[]
  onToggle: (todoId: string) => void
  onDelete: (todoId: string) => void
  planId: string
  onRefresh: () => void
}

export function TodoList({ todos, onToggle, onDelete, planId, onRefresh }: TodoListProps) {
  if (todos.length === 0) {
    return (
      <div className="flex h-14 items-center justify-center rounded-lg border-2 border-dashed border-slate-200 text-xs text-slate-400">
        No tasks yet
      </div>
    )
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
          allTodos={todos}
          onRefresh={onRefresh}
        />
      ))}
    </div>
  )
}

function TodoItemRow({
  todo,
  onToggle,
  onDelete,
  planId,
  allTodos,
  onRefresh,
}: {
  todo: TodoItemWithDeps
  onToggle: (todoId: string) => void
  onDelete: (todoId: string) => void
  planId: string
  allTodos: TodoItemWithDeps[]
  onRefresh: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [showEditDeps, setShowEditDeps] = useState(false)

  const depsCount = (todo.depends_on ?? []).length
  const blockersCount = (todo.blockers ?? []).length
  const blocked = depsCount > 0 && !todo.completed

  return (
    <>
      <div
        className={cn(
          'group relative flex items-start gap-2.5 rounded-lg px-3 py-2.5 transition-colors bg-white border-b border-slate-100',
          'hover:bg-slate-50'
        )}
      >
        <GripVertical className="mt-0.5 h-4 w-4 shrink-0 cursor-grab text-slate-300/50 group-hover:text-slate-400 transition-colors" />

        <Checkbox
          checked={todo.completed}
          onCheckedChange={() => onToggle(todo.id)}
          disabled={blocked && !todo.completed}
          className={cn(
            'mt-0.5 h-4 w-4 shrink-0 rounded-sm border-slate-300',
            todo.completed &&
              'text-emerald-500 data-[state=checked]:bg-emerald-500 data-[state=checked]:border-emerald-500'
          )}
        />

        <div className="min-w-0 flex-1 pt-px">
          <span
            className={cn(
              'text-[13px] leading-relaxed',
              todo.completed && 'line-through text-slate-400'
            )}
          >
            {todo.content}
          </span>
        </div>

        <div className="flex shrink-0 items-center gap-1 pt-0.5">
          {depsCount > 0 && (
            <button
              type="button"
              onClick={() => setExpanded(!expanded)}
              className={cn(
                'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium transition-colors',
                expanded
                  ? 'bg-blue-200 text-blue-700'
                  : 'bg-blue-100 text-blue-700 hover:bg-blue-200'
              )}
            >
              <GitBranch className="h-2.5 w-2.5" />
              {depsCount}
            </button>
          )}

          {blockersCount > 0 && (
            <span className="inline-flex items-center gap-1 rounded-md bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700">
              <AlertTriangle className="h-2.5 w-2.5" />
              {blockersCount}
            </span>
          )}

          {!expanded && depsCount > 0 && !blocked && !todo.completed && (
            <button
              type="button"
              onClick={() => setShowEditDeps(true)}
              className="inline-flex items-center justify-center h-5 w-5 rounded text-muted-foreground/30 hover:text-muted-foreground transition-colors opacity-0 group-hover:opacity-100"
              title="Edit dependencies"
            >
              <Pencil className="h-2.5 w-2.5" />
            </button>
          )}

          {blocked && !todo.completed && (
            <Tooltip>
              <TooltipTrigger className="inline-flex">
                <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
              </TooltipTrigger>
              <TooltipContent side="top" className="text-xs">
                Blocked by unfinished dependencies
              </TooltipContent>
            </Tooltip>
          )}

          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => onDelete(todo.id)}
            className="h-6 w-6 text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        </div>

        {todo.completed && (
          <CheckCircle2 className="mt-0.5 absolute right-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-emerald-400 opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none" />
        )}
      </div>

      {expanded && (
        <div className="ml-9 mb-1 rounded-lg bg-slate-50 border border-slate-200 animate-in fade-in slide-in-from-top-1 duration-150">
          <div className="max-h-48 overflow-y-auto">
            <DependencyChain todos={allTodos} selectedTodoId={todo.id} planId={planId} />
          </div>
          <div className="px-3 pb-2 flex items-center gap-2">
            <button
              type="button"
              onClick={() => setShowEditDeps(true)}
              className="inline-flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground transition-colors"
            >
              <Pencil className="h-2.5 w-2.5" />
              Edit dependencies
            </button>
            <button
              type="button"
              onClick={() => setExpanded(false)}
              className="inline-flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground transition-colors"
            >
              Collapse
            </button>
          </div>
        </div>
      )}

      {showEditDeps && (
        <DependencyEditDialog
          open={showEditDeps}
          onClose={() => setShowEditDeps(false)}
          todos={allTodos}
          selectedTodoId={todo.id}
          onSaved={onRefresh}
        />
      )}
    </>
  )
}
