import { Outlet, useLocation } from 'react-router-dom'
import { Settings } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'

const tabLabels: Record<string, string> = {
  '/settings/config': 'Global Configuration Parameters',
  '/settings/profile': 'Developer Profile',
  '/settings/skills': 'Agent Skill Control',
  '/settings/mcp': 'MCP Model Context Service',
  '/settings/teams': 'Team Members & Workgroups',
  '/settings/projects': 'Project Workspaces',
  '/settings/proxies': 'Proxy Debugger',
}

export function SettingsLayout() {
  const location = useLocation()
  const activeLabel = tabLabels[location.pathname] || 'Preferences'
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