import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { LogOut, FileText, User, Sparkles, Server, Users, Monitor, FolderOpen } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/authStore'

const settingsTabs = [
  { to: '/settings/config', icon: FileText, label: 'Config' },
  { to: '/settings/profile', icon: User, label: 'Profile' },
  { to: '/settings/skills', icon: Sparkles, label: 'Skills' },
  { to: '/settings/mcp', icon: Server, label: 'MCP' },
  { to: '/settings/teams', icon: Users, label: 'Agents & Teams' },
  { to: '/settings/projects', icon: FolderOpen, label: 'Projects' },
  { to: '/settings/proxies', icon: Monitor, label: 'Proxies' },
]

export function SettingsLayout() {
  const { user, logout } = useAuthStore()
  const location = useLocation()
  const navigate = useNavigate()

  return (
    <div className="h-full overflow-y-auto px-3 py-3 sm:px-6 sm:py-6 pb-20 md:pb-6">
      <div className="mx-auto max-w-3xl">
        {/* Mobile-only header for settings */}
        <div className="flex md:hidden flex-col gap-4 border-b border-border/40 pb-3 mb-5">
          <div className="flex items-center justify-between">
            <div className="flex flex-col">
              <span className="text-sm font-bold text-foreground">Settings</span>
              <span className="text-[10px] text-muted-foreground">Logged in as {user}</span>
            </div>
            <Button
              variant="ghost"
              size="xs"
              className="h-8 gap-1.5 text-muted-foreground hover:text-destructive"
              onClick={logout}
            >
              <LogOut className="h-3.5 w-3.5" />
              Logout
            </Button>
          </div>

          {/* Mobile submenus navigation */}
          <div className="flex overflow-x-auto no-scrollbar gap-2 py-1">
            {settingsTabs.map((tab) => {
              const isActive = location.pathname === tab.to
              return (
                <button
                  key={tab.to}
                  onClick={() => navigate(tab.to)}
                  className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded-md border transition-all duration-200 whitespace-nowrap ${
                    isActive
                      ? 'bg-primary text-primary-foreground border-primary shadow-sm'
                      : 'bg-card/40 border-border/80 text-muted-foreground hover:text-foreground hover:bg-muted/40'
                  }`}
                >
                  <tab.icon className="h-3.5 w-3.5" />
                  {tab.label}
                </button>
              )
            })}
          </div>
        </div>
        <Outlet />
      </div>
    </div>
  )
}
