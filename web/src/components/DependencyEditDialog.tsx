import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import type { TodoItemWithDeps } from '@/types'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Loader2 } from 'lucide-react'
import { setDependencies as apiSetDependencies } from '@/lib/api'

interface DependencyEditDialogProps {
  open: boolean
  onClose: () => void
  todos: TodoItemWithDeps[]
  selectedTodoId: string
  onSaved: () => void
}

export function DependencyEditDialog({
  open,
  onClose,
  todos,
  selectedTodoId,
  onSaved,
}: DependencyEditDialogProps) {
  const current = todos.find((t) => t.id === selectedTodoId)
  const [selected, setSelected] = useState<string[]>([])
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (open && current) {
      setSelected([...(current.depends_on ?? [])])
    }
  }, [open, current])

  async function handleSave() {
    if (!current) return
    setSaving(true)
    try {
      await apiSetDependencies(selectedTodoId, { depends_on: selected })
      onSaved()
      onClose()
    } catch {
      // error handled by api layer
    } finally {
      setSaving(false)
    }
  }

  const candidates = todos.filter((t) => t.id !== selectedTodoId)

  return (
    <Dialog open={open} onOpenChange={(v) => !v && !saving && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-base">Edit Dependencies</DialogTitle>
        </DialogHeader>

        {current && (
          <p className="text-xs text-muted-foreground -mt-2">
            Select tasks that{' '}
            <span className="font-medium text-foreground">&ldquo;{current.content}&rdquo;</span>{' '}
            depends on:
          </p>
        )}

        <div className="max-h-64 overflow-y-auto space-y-1 -mx-1 px-1">
          {candidates.length === 0 ? (
            <p className="text-xs text-muted-foreground/60 py-4 text-center">
              No other tasks available
            </p>
          ) : (
            candidates.map((todo) => (
              <label
                key={todo.id}
                className={cn(
                  'flex items-center gap-2.5 rounded-md px-3 py-2 cursor-pointer transition-colors',
                  'hover:bg-slate-50',
                  selected.includes(todo.id) && 'bg-blue-50 ring-1 ring-blue-200'
                )}
              >
                <Checkbox
                  checked={selected.includes(todo.id)}
                  onCheckedChange={(checked) => {
                    setSelected((prev) =>
                      checked ? [...prev, todo.id] : prev.filter((id) => id !== todo.id)
                    )
                  }}
                  className="h-4 w-4 rounded-sm"
                />
                <div className="flex items-center gap-2 min-w-0 flex-1">
                  <div
                    className={cn(
                      'w-1.5 h-1.5 rounded-full shrink-0',
                      todo.completed ? 'bg-emerald-500' : 'bg-blue-500'
                    )}
                  />
                  <span
                    className={cn(
                      'text-sm truncate',
                      todo.completed && 'line-through text-slate-400'
                    )}
                  >
                    {todo.content}
                  </span>
                </div>
              </label>
            ))
          )}
        </div>

        <div className="flex items-center justify-end gap-2 pt-2 border-t border-border">
          <Button variant="outline" size="sm" onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSave} disabled={saving || candidates.length === 0}>
            {saving ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                Saving...
              </>
            ) : (
              'Save'
            )}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
