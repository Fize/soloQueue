import { useMemo } from 'react'
import type { TodoItemWithDeps } from '@/types'
import { cn } from '@/lib/utils'
import { CheckCircle2, Clock, ArrowUp, ArrowDown } from 'lucide-react'

interface DependencyChainProps {
  todos: TodoItemWithDeps[]
  selectedTodoId: string
  planId: string
}

function todoById(todos: TodoItemWithDeps[], id: string): TodoItemWithDeps | undefined {
  return todos.find((t) => t.id === id)
}

type NodeType = 'depends' | 'current' | 'blocker'

interface ChainNode {
  todo: TodoItemWithDeps
  type: NodeType
  hasUp: boolean
  hasDown: boolean
}

export function DependencyChain({ todos, selectedTodoId }: DependencyChainProps) {
  const current = todoById(todos, selectedTodoId)

  const { chain } = useMemo(() => {
    if (!current) return { chain: [] as ChainNode[] }

    const dependsItems = (current.depends_on ?? [])
      .map((id) => todoById(todos, id))
      .filter(Boolean) as TodoItemWithDeps[]

    const blockerItems = (current.blockers ?? [])
      .map((id) => todoById(todos, id))
      .filter(Boolean) as TodoItemWithDeps[]

    const nodes: ChainNode[] = [
      ...dependsItems.map((t) => ({
        todo: t,
        type: 'depends' as NodeType,
        hasUp: dependsItems.length > 1,
        hasDown: true,
      })),
      {
        todo: current,
        type: 'current' as NodeType,
        hasUp: dependsItems.length > 0,
        hasDown: blockerItems.length > 0,
      },
      ...blockerItems.map((t) => ({
        todo: t,
        type: 'blocker' as NodeType,
        hasUp: true,
        hasDown: blockerItems.length > 1,
      })),
    ]

    nodes.forEach((n, i) => {
      n.hasUp = i > 0
      n.hasDown = i < nodes.length - 1
    })

    return { chain: nodes }
  }, [todos, current])

  if (!current || chain.length <= 1) {
    return <div className="text-xs text-muted-foreground/60 italic px-3">No dependencies</div>
  }

  return (
    <div className="relative py-2">
      {chain.map((node, i) => (
        <div key={node.todo.id} className="flex items-stretch gap-3">
          {/* Gutter with connector */}
          <div className="flex flex-col items-center w-5 shrink-0">
            {/* Up connector */}
            <div
              className={cn(
                'w-0.5 transition-colors duration-300',
                node.hasUp
                  ? node.todo.completed
                    ? 'bg-emerald-400'
                    : 'bg-slate-300'
                  : 'bg-transparent',
                i === 0 ? 'h-0' : 'h-3'
              )}
            />
            {/* Dot */}
            <div
              className={cn(
                'w-3.5 h-3.5 rounded-full shrink-0 ring-2 ring-background z-10 flex items-center justify-center transition-all duration-300',
                node.type === 'current' && 'ring-blue-300/50',
                node.todo.completed
                  ? 'bg-emerald-500'
                  : (node.todo.depends_on ?? []).length > 0 && !node.todo.completed
                    ? 'bg-amber-500'
                    : 'bg-blue-500'
              )}
            >
              {node.todo.completed ? (
                <CheckCircle2 className="h-2.5 w-2.5 text-white" />
              ) : (
                <div className="h-1.5 w-1.5 rounded-full bg-white" />
              )}
            </div>
            {/* Down connector */}
            <div
              className={cn(
                'w-0.5 transition-colors duration-300',
                node.hasDown
                  ? node.todo.completed
                    ? 'bg-emerald-400'
                    : 'bg-slate-300'
                  : 'bg-transparent',
                i === chain.length - 1 ? 'h-0' : 'h-3'
              )}
            />
          </div>

          {/* Node content */}
          <div
            className={cn(
              'flex-1 min-w-0 rounded-lg px-3 py-2 transition-all duration-200',
              node.type === 'current'
                ? 'bg-blue-50 ring-1 ring-blue-200'
                : 'bg-white hover:bg-slate-50',
              node.todo.completed && 'opacity-60'
            )}
          >
            <div className="flex items-center gap-2">
              <span
                className={cn(
                  'text-xs leading-snug',
                  node.todo.completed ? 'line-through text-slate-400' : 'text-slate-700',
                  node.type === 'current' && 'font-medium'
                )}
              >
                {node.todo.content}
              </span>
              {node.type === 'depends' && (
                <span className="inline-flex items-center gap-0.5 text-[10px] text-blue-600 font-medium shrink-0">
                  <ArrowUp className="h-2.5 w-2.5" />
                  prerequisite
                </span>
              )}
              {node.type === 'blocker' && (
                <span className="inline-flex items-center gap-0.5 text-[10px] text-amber-600 font-medium shrink-0">
                  <ArrowDown className="h-2.5 w-2.5" />
                  dependent
                </span>
              )}
            </div>
            {!node.todo.completed && (node.todo.depends_on ?? []).length > 0 && (
              <div className="flex items-center gap-1 mt-1">
                <Clock className="h-2.5 w-2.5 text-amber-500" />
                <span className="text-[10px] text-amber-600">
                  Waiting on {(node.todo.depends_on ?? []).length} prerequisite
                  {(node.todo.depends_on ?? []).length > 1 ? 's' : ''}
                </span>
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}
