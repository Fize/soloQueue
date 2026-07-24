import { Box } from 'lucide-react'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import type { SandboxConfig } from '@/types'

interface SandboxSectionProps {
  config: SandboxConfig
  onChange: (config: SandboxConfig) => void
  onSave: () => void
}

export function SandboxSection({ config, onChange, onSave }: SandboxSectionProps) {
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Box className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">Docker Sandbox</h3>
            <span className="text-[10px] font-mono uppercase bg-amber-500/15 text-amber-600 dark:text-amber-400 px-2 py-0.5 rounded border border-amber-500/30">
              实验性功能 / Experimental
            </span>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            在 Docker Linux 隔离容器中安全运行 Bash 命令行，避免污染宿主机环境。支持加载 ~/.soloqueue/sandbox/ 下的 Dockerfile 与 meta.json。
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          保存设置
        </Button>
      </div>

      <div className="flex items-center justify-between pt-2">
        <div className="flex flex-col gap-0.5">
          <span className="text-sm font-semibold text-foreground">启用 Docker Sandbox</span>
          <span className="text-xs text-muted-foreground">
            开启后，Bash 命令将在基于 Debian/Linux 的隔离 Docker 容器内执行（工作目录 1:1 映射，自动挂载 ~/.ssh 与持久缓存）。
          </span>
        </div>
        <Switch
          checked={config.enabled}
          onCheckedChange={(val) => onChange({ ...config, enabled: val })}
        />
      </div>
    </div>
  )
}
