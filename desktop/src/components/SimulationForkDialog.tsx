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

  const [forkTopic, setForkTopic] = useState(initialTopic)
  const [forkMaxWallClockMin, setForkMaxWallClockMin] = useState(initialMaxWallClockMin)
  const [forking, setForking] = useState(false)

  // Reset state when dialog opens
  useEffect(() => {
    if (open) {
      setForkTopic(initialTopic)
      setForkMaxWallClockMin(initialMaxWallClockMin)
      setForking(false)
    }
  }, [open, initialTopic, initialMaxWallClockMin])

  const handleFork = async () => {
    try {
      setForking(true)
      const res = await fetch(`/api/simulations/${simulationId}/fork`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          new_topic: forkTopic,
          new_max_wall_clock_ms: forkMaxWallClockMin * 60 * 1000,
        }),
      })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || '分叉仿真失败')
      }
      const data = await res.json()
      toast.success('仿真分叉成功！')
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
            分叉仿真
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="rounded-lg bg-background/40 border border-border p-3 text-[10px] text-muted-foreground leading-relaxed">
            分叉操作将克隆当前仿真的配置（包括所有智能体画像、初始社会关系与运行参数）到一个新的沙盒中，以便您可以对其发起对照测试和差异演化研究。
          </div>

          <div className="space-y-1.5">
            <Input
              label="新仿真主题"
              value={forkTopic}
              onChange={(e) => setForkTopic(e.target.value)}
              required
              className="text-xs"
            />
          </div>

          <div className="space-y-1.5">
            <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono flex justify-between items-center">
              <span>最大运行时间 (分钟)</span>
              <span className="text-primary font-bold">{forkMaxWallClockMin}分钟</span>
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
            取消
          </button>
          <button
            type="button"
            onClick={handleFork}
            disabled={forking || !forkTopic.trim()}
            className="flex items-center justify-center gap-1.5 rounded-lg bg-primary hover:bg-primary/95 disabled:bg-primary/50 px-4 py-2 text-xs font-semibold text-primary-foreground transition-all cursor-pointer shadow-md shadow-primary/5 disabled:cursor-not-allowed"
          >
            {forking ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" /> 分叉中...
              </>
            ) : (
              <>
                <GitFork className="h-3.5 w-3.5" /> 分叉仿真
              </>
            )}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
