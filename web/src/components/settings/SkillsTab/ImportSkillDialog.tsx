import { AlertTriangle, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface ImportSkillDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  name: string
  onNameChange: (val: string) => void
  description: string
  onDescriptionChange: (val: string) => void
  triggers: string
  onTriggersChange: (val: string) => void
  body: string
  onBodyChange: (val: string) => void
  onSave: () => Promise<void>
  saving: boolean
  error: string | null
}

export function ImportSkillDialog({
  open,
  onOpenChange,
  name,
  onNameChange,
  description,
  onDescriptionChange,
  triggers,
  onTriggersChange,
  body,
  onBodyChange,
  onSave,
  saving,
  error,
}: ImportSkillDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="md:max-w-3xl w-[95vw] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Create Custom Skill</DialogTitle>
          <DialogDescription>
            Write details and instructions for a new AI skill. It will be stored inside your user
            skills directory.
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 my-2 text-left">
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Input
                label="Skill ID / Folder name"
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder="e.g. search-web"
              />
              <span className="text-[10px] text-muted-foreground">
                Allowed characters: a-z, 0-9, dash, underscore.
              </span>
            </div>

            <div className="flex flex-col gap-1.5">
              <Textarea
                label="Description"
                value={description}
                onChange={(e) => onDescriptionChange(e.target.value)}
                rows={2}
                placeholder="What is the purpose of this skill?"
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <Input
                label="Triggers (comma-separated)"
                value={triggers}
                onChange={(e) => onTriggersChange(e.target.value)}
                placeholder="search the web, query search"
              />
            </div>
          </div>

          <div className="flex flex-col gap-1.5 min-h-[220px]">
            <Textarea
              label="SKILL.md Markdown Body"
              value={body}
              onChange={(e) => onBodyChange(e.target.value)}
              className="flex-1 w-full font-mono text-xs"
              placeholder="# Instructions for using this skill\n\n1. First do X\n2. Next do Y"
              spellCheck={false}
            />
          </div>
        </div>

        {error && (
          <p className="text-xs text-destructive text-left flex items-center gap-1">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
            {error}
          </p>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={onSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                Creating...
              </>
            ) : (
              'Create Skill'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
