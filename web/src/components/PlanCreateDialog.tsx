import { useState } from 'react'
import { usePlanStore } from '@/stores/planStore'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Button } from '@/components/ui/button'
import { Loader2, Plus } from 'lucide-react'

interface PlanCreateDialogProps {
  open: boolean
  onClose: () => void
}

const statusOptions = [
  { value: 'plan', label: 'Plan' },
  { value: 'running', label: 'Running' },
  { value: 'done', label: 'Done' },
]

export function PlanCreateDialog({ open, onClose }: PlanCreateDialogProps) {
  const createPlan = usePlanStore((s) => s.createPlan)
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [tags, setTags] = useState('')
  const [status, setStatus] = useState('plan')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit() {
    if (!title.trim()) {
      setError('Title is required')
      return
    }
    setSaving(true)
    setError(null)
    try {
      await createPlan({
        title: title.trim(),
        content: content.trim() || undefined,
        tags: tags.trim() || undefined,
        status,
        creator: 'user',
      })
      setTitle('')
      setContent('')
      setTags('')
      setStatus('plan')
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create plan')
    } finally {
      setSaving(false)
    }
  }

  function handleClose() {
    if (saving) return
    setTitle('')
    setContent('')
    setTags('')
    setStatus('plan')
    setError(null)
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plus className="h-4 w-4" />
            New Plan
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <Input
            label="Title *"
            placeholder="Plan title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            error={error ?? undefined}
          />

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">Content</label>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="Markdown content (optional)"
              rows={6}
              className="w-full resize-y rounded-md border border-border bg-transparent px-3 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50"
            />
          </div>

          <div className="flex gap-3">
            <div className="flex-1">
              <Select
                label="Status"
                options={statusOptions}
                value={status}
                onChange={setStatus}
              />
            </div>
          </div>

          <Input
            label="Tags"
            placeholder="Comma-separated, e.g. bug,frontend"
            value={tags}
            onChange={(e) => setTags(e.target.value)}
          />
        </div>

        <div className="flex items-center justify-between border-t border-border pt-4">
          {error && <p className="text-xs text-destructive">{error}</p>}
          <div className="flex items-center gap-2 ml-auto">
            <Button variant="outline" size="sm" onClick={handleClose} disabled={saving}>
              Cancel
            </Button>
            <Button size="sm" onClick={handleSubmit} disabled={saving}>
              {saving ? (
                <>
                  <Loader2 className="mr-1 h-3 w-3 animate-spin" /> Creating...
                </>
              ) : (
                'Create'
              )}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
