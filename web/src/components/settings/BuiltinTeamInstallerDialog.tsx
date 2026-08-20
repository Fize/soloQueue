import { useEffect, useMemo, useState } from 'react'
import { Boxes, Loader2, UserRound } from 'lucide-react'
import { listBuiltinTeams, installBuiltinTeams } from '@/lib/api'
import { APIError } from '@/lib/api/core'
import type { BuiltinTeamCatalogItem } from '@/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'

interface BuiltinTeamInstallerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onInstalled: () => void | Promise<void>
}

export function BuiltinTeamInstallerDialog({
  open,
  onOpenChange,
  onInstalled,
}: BuiltinTeamInstallerDialogProps) {
  const { t } = useTranslation()
  const [teams, setTeams] = useState<BuiltinTeamCatalogItem[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [installing, setInstalling] = useState(false)

  useEffect(() => {
    if (!open) return
    let active = true
    listBuiltinTeams()
      .then((items) => {
        if (active) setTeams(items)
      })
      .catch((err) => {
        console.error(err)
        toast.error(t('teams.builtinLoadFailed'))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [open, t])

  const selectedSet = useMemo(() => new Set(selected), [selected])

  const toggleTeam = (id: string, checked: boolean) => {
    setSelected((current) =>
      checked ? [...current, id] : current.filter((item) => item !== id),
    )
  }

  const handleInstall = async () => {
    if (selected.length === 0) return
    setInstalling(true)
    try {
      const result = await installBuiltinTeams(selected)
      await onInstalled()
      if (result.restart_required) {
        toast.warning(t('teams.builtinInstalledRestart'))
      } else {
        toast.success(t('teams.builtinInstalled', { count: result.results.length }))
      }
      onOpenChange(false)
    } catch (err) {
      if (err instanceof APIError && err.code === 'session_busy') {
        toast.error(t('teams.builtinSessionBusy'))
      } else if (err instanceof APIError && err.code === 'builtin_team_conflict') {
        toast.error(t('teams.builtinConflict'))
      } else {
        toast.error(t('teams.builtinInstallFailed'))
      }
    } finally {
      setInstalling(false)
    }
  }

  const statusLabel = (status: BuiltinTeamCatalogItem['status']) =>
    t(`teams.builtinStatus.${status}`)

  const catalogText = (team: BuiltinTeamCatalogItem, field: 'name' | 'description') => {
    const key = `teams.builtinCatalog.${team.id}.${field}`
    const translated = t(key)
    if (translated !== key) return translated
    return field === 'name' ? team.display_name : team.description
  }

  return (
    <Dialog open={open} onOpenChange={installing ? undefined : onOpenChange}>
      <DialogContent className="max-w-[min(92vw,720px)]" showCloseButton={!installing}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Boxes className="h-4 w-4" />
            {t('teams.installBuiltinTeams')}
          </DialogTitle>
          <DialogDescription>{t('teams.installBuiltinTeamsDesc')}</DialogDescription>
        </DialogHeader>

        {loading ? (
          <div className="flex min-h-40 items-center justify-center text-muted-foreground">
            <Loader2 className="h-5 w-5 animate-spin" />
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {teams.map((team) => {
              const selectable = team.status === 'available' || team.status === 'partial'
              const checked = selectedSet.has(team.id)
              const displayName = catalogText(team, 'name')
              return (
                <label
                  key={team.id}
                  className={`rounded-lg border p-4 transition-colors ${
                    selectable
                      ? 'cursor-pointer hover:bg-muted/40'
                      : 'cursor-default bg-muted/20 opacity-75'
                  } ${checked ? 'border-primary bg-primary/5' : 'border-border'}`}
                >
                  <div className="flex items-start gap-3">
                    <Checkbox
                      checked={checked}
                      disabled={!selectable || installing}
                      onCheckedChange={(value) => toggleTeam(team.id, value)}
                      aria-label={displayName}
                    />
                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex items-center justify-between gap-2">
                        <div>
                          <p className="font-medium text-foreground">{displayName}</p>
                          <p className="text-[11px] text-muted-foreground">{team.name}</p>
                        </div>
                        <Badge variant={team.status === 'installed' ? 'success' : 'secondary'}>
                          {statusLabel(team.status)}
                        </Badge>
                      </div>
                      <p className="text-xs leading-relaxed text-muted-foreground">
                        {catalogText(team, 'description')}
                      </p>
                      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <UserRound className="h-3.5 w-3.5" />
                        <span>{team.leader}</span>
                        <span>·</span>
                        <span>{t('teams.builtinMemberCount', { count: team.members.length })}</span>
                      </div>
                      {team.status === 'partial' && team.missing_agents.length > 0 && (
                        <p className="text-xs text-amber-600">
                          {t('teams.builtinMissing', { names: team.missing_agents.join(', ') })}
                        </p>
                      )}
                      {team.status === 'conflict' && team.conflicts.length > 0 && (
                        <p className="text-xs text-destructive">{t('teams.builtinConflict')}</p>
                      )}
                    </div>
                  </div>
                </label>
              )
            })}
          </div>
        )}

        <DialogFooter>
          <Button disabled={installing || selected.length === 0} onClick={handleInstall}>
            {installing && <Loader2 className="h-4 w-4 animate-spin" />}
            {t('teams.installSelectedBuiltinTeams', { count: selected.length })}
          </Button>
          <Button
            variant="outline"
            disabled={installing}
            onClick={() => onOpenChange(false)}
          >
            {t('common.cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
