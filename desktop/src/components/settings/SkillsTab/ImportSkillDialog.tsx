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
import { useTranslation } from '@/lib/i18n'

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
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl w-[95vw] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('skills.importTitle')}</DialogTitle>
          <DialogDescription>
            {t('skills.importDesc')}
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 my-2 text-left">
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Input
                label={t('skills.skillIdLabel')}
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder={t('skills.skillIdPlaceholder')}
              />
              <span className="text-[10px] text-muted-foreground">
                {t('skills.skillIdHelp')}
              </span>
            </div>

            <div className="flex flex-col gap-1.5">
              <Textarea
                label={t('skills.descLabel')}
                value={description}
                onChange={(e) => onDescriptionChange(e.target.value)}
                rows={2}
                placeholder={t('skills.descPlaceholder')}
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <Input
                label={t('skills.triggersLabel')}
                value={triggers}
                onChange={(e) => onTriggersChange(e.target.value)}
                placeholder={t('skills.triggersPlaceholder')}
              />
            </div>
          </div>

          <div className="flex flex-col gap-1.5 min-h-[220px]">
            <Textarea
              label={t('skills.bodyLabel')}
              value={body}
              onChange={(e) => onBodyChange(e.target.value)}
              className="flex-1 w-full font-mono text-xs"
              placeholder={t('skills.bodyPlaceholder')}
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
            {t('common.cancel')}
          </Button>
          <Button onClick={onSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                {t('skills.creating')}
              </>
            ) : (
              t('skills.createSkill')
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
