import { Outlet, useLocation } from 'react-router-dom'
import { Settings } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'

const tabLabels: Record<string, string> = {
  '/settings/config': '全局配置参数',
  '/settings/profile': '开发者个人资料',
  '/settings/skills': '智能体技能控制',
  '/settings/mcp': 'MCP 模型上下文服务',
  '/settings/teams': '团队成员与工作组',
  '/settings/projects': '项目工作空间',
  '/settings/proxies': '代理调试器',
}

export function SettingsLayout() {
  const location = useLocation()
  const activeLabel = tabLabels[location.pathname] || '偏好设置'
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed)

  return (
    <div className="h-full w-full overflow-hidden flex flex-col bg-background">
      {/* macOS Style Preferences Header Bar */}
      <header className={cn(
        "flex h-12 items-center justify-between border-b border-border/30 px-6 bg-card/20 shrink-0 select-none",
        sidebarCollapsed && "pl-[115px]"
      )}>
        <div className="flex items-center gap-2.5">
          <Settings className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-xs font-bold text-foreground font-mono">{activeLabel}</h1>
        </div>
      </header>

      {/* Preferences Content Canvas */}
      <div className="flex-1 overflow-y-auto bg-background">
        <div className="flex justify-center p-6 md:p-8">
          <div className="w-full max-w-4xl bg-card rounded-lg border border-border/40 p-6 md:p-8 shadow-sm">
            <Outlet />
          </div>
        </div>
      </div>
    </div>
  )
}
export default SettingsLayout
