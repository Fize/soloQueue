import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { GitFork, Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { useTranslation } from '@/lib/i18n'
import { forkSimulation } from '@/lib/api'

interface SimulationForkDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  simulationId: string
  initialTopic: string
  initialMaxWallClockMin: number
}

export function SimulationForkDialog({
  open,
  onOpenChange,
  simulationId,
  initialTopic,
  initialMaxWallClockMin,
}: SimulationForkDialogProps) {
  const navigate = useNavigate()
  const [forkTopic, setForkTopic] = useState('')
  const [forkMaxWallClockMin, setForkMaxWallClockMin] = useState(5)
  const [forking, setForking] = useState(false)
  const { t } = useTranslation()

  // Reset/sync when dialog opens
  useEffect(() => {
    if (open) {
      setForkTopic(`${initialTopic} (Copy)`)
      setForkMaxWallClockMin(initialMaxWallClockMin || 5)
    }
  }, [open, initialTopic, initialMaxWallClockMin])

  const handleFork = async () => {
    setForking(true)
    try {
      const data = await forkSimulation(simulationId, {
          new_topic: forkTopic,
          new_max_wall_clock_ms: forkMaxWallClockMin * 60 * 1000,
      })
      toast.success(t('simulation.forkSuccess'))
      onOpenChange(false)
      navigate(`/simulations/${data.new_simulation_id}`)
    } catch (err: any) {
      toast.error(err.message)
    } finally {
      setForking(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md bg-card border border-border rounded-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm font-bold text-foreground">
            <GitFork className="h-4.5 w-4.5 text-primary" />
            {t('simulation.fork')}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="rounded-lg bg-background/40 border border-border p-3 text-[10px] text-muted-foreground leading-relaxed">
            {t('simulation.forkDesc')}
          </div>

          <div className="space-y-1.5">
            <Input
              label={t('simulation.forkTheme')}
              value={forkTopic}
              onChange={(e) => setForkTopic(e.target.value)}
              required
              className="text-xs"
            />
          </div>

          <div className="space-y-1.5">
            <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono flex justify-between items-center">
              <span>{t('simulation.forkMaxTime')}</span>
              <span className="text-primary font-bold">{t('simulation.minutesUnit', { count: forkMaxWallClockMin })}</span>
            </label>
            <div className="flex items-center gap-2">
              <input
                type="range"
                min={1}
                max={180}
                value={Math.min(forkMaxWallClockMin, 180)}
                onChange={(e) => setForkMaxWallClockMin(parseInt(e.target.value) || 5)}
                className="flex-1 h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
              />
              <Input
                type="number"
                min={1}
                max={1440}
                value={forkMaxWallClockMin}
                onChange={(e) => {
                  const val = Math.max(1, Math.min(1440, parseInt(e.target.value) || 1))
                  setForkMaxWallClockMin(val)
                }}
                className="w-16 text-center text-xs h-7 py-1 px-1.5 shrink-0"
              />
            </div>
          </div>
        </div>

        <DialogFooter showCloseButton={false}>
          <button
            type="button"
            onClick={() => onOpenChange(false)}
            disabled={forking}
            className="rounded-lg bg-muted hover:bg-muted/80 px-4 py-2 text-xs font-semibold text-foreground transition-colors cursor-pointer"
          >
            {t('common.cancel')}
          </button>
          <button
            type="button"
            onClick={handleFork}
            disabled={forking || !forkTopic.trim()}
            className="flex items-center justify-center gap-1.5 rounded-lg bg-primary hover:bg-primary/95 disabled:bg-primary/50 px-4 py-2 text-xs font-semibold text-primary-foreground transition-all cursor-pointer shadow-md shadow-primary/5 disabled:cursor-not-allowed"
          >
            {forking ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" /> {t('simulation.forking')}
              </>
            ) : (
              <>
                <GitFork className="h-3.5 w-3.5" /> {t('simulation.fork')}
              </>
            )}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
