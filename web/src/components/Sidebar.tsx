import { useEffect, useState, useRef } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useRuntime } from '@/hooks/useRuntime'
import { Button } from '@/components/ui/button'
import {
  LayoutDashboard,
  Kanban,
  FolderOpen,
  Settings,
  ChevronDown,
  ChevronRight,
  FileText,
  User,
  Sparkles,
  Server,
  Circle,
  X,
} from 'lucide-react'

const mainNav = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/plans', icon: Kanban, label: 'Plans' },
  { to: '/files', icon: FolderOpen, label: 'Files' },
]

const settingsChildren = [
  { to: '/settings/config', icon: FileText, label: 'Config' },
  { to: '/settings/profile', icon: User, label: 'Profile' },
  { to: '/settings/skills', icon: Sparkles, label: 'Skills' },
  { to: '/settings/mcp', icon: Server, label: 'MCP' },
]

function NavItem({
  to,
  icon: Icon,
  label,
  active,
}: {
  to: string
  icon: typeof LayoutDashboard
  label: string
  active: boolean
}) {
  const navigate = useNavigate()
  return (
    <button
      onClick={() => navigate(to)}
      className={cn(
        'flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
        active
          ? 'bg-primary/10 text-primary border-l-2 border-primary ml-0 pl-[10px]'
          : 'text-muted-foreground hover:text-foreground hover:bg-muted ml-0 pl-3'
      )}
    >
      <Icon className="h-4 w-4 shrink-0" />
      {label}
    </button>
  )
}

export function Sidebar({ mobile, onClose }: { mobile?: boolean; onClose?: () => void }) {
  const location = useLocation()
  const navigate = useNavigate()
  const runtime = useRuntime()

  const isSettingsActive = location.pathname.startsWith('/settings')
  const [settingsOpen, setSettingsOpen] = useState(isSettingsActive)

  useEffect(() => {
    if (isSettingsActive && !settingsOpen) {
      setSettingsOpen(true)
    }
  }, [isSettingsActive, settingsOpen])

  // Close mobile overlay on route change (skip initial mount)
  const lastPathRef = useRef(location.pathname)
  useEffect(() => {
    if (location.pathname !== lastPathRef.current) {
      lastPathRef.current = location.pathname
      if (mobile) onClose?.()
    }
  }, [location.pathname, mobile, onClose])

  return (
    <aside
      className={cn(
        'flex h-full shrink-0 flex-col border-r border-border bg-card',
        mobile ? 'fixed inset-y-0 left-0 z-50 w-[280px] shadow-2xl' : 'w-[230px]'
      )}
    >
      {/* Logo */}
      <div className="flex h-14 items-center gap-2.5 px-5 border-b border-border">
        {mobile && (
          <Button variant="ghost" size="icon-sm" onClick={onClose} className="mr-1 shrink-0">
            <X className="h-4 w-4" />
          </Button>
        )}
        <Link to="/" className="flex items-center gap-2.5 min-w-0">
          <img src="/logo.png" alt="SoloQueue" className="h-7 w-7 shrink-0" />
          <span className="text-base font-bold tracking-tight text-foreground truncate">
            SoloQueue
          </span>
        </Link>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto p-3 space-y-0.5">
        {mainNav.map((item) => (
          <NavItem
            key={item.to}
            to={item.to}
            icon={item.icon}
            label={item.label}
            active={
              item.to === '/' ? location.pathname === '/' : location.pathname.startsWith(item.to)
            }
          />
        ))}

        {/* Settings with submenu */}
        <div className="pt-2">
          <button
            onClick={() => setSettingsOpen(!settingsOpen)}
            className={cn(
              'flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
              isSettingsActive
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted'
            )}
          >
            <Settings className="h-4 w-4 shrink-0" />
            <span className="flex-1 text-left">Settings</span>
            {settingsOpen ? (
              <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
            )}
          </button>

          {settingsOpen && (
            <div className="mt-0.5 space-y-0.5 pl-3">
              {settingsChildren.map((item) => {
                const active = location.pathname === item.to
                return (
                  <button
                    key={item.to}
                    onClick={() => navigate(item.to)}
                    className={cn(
                      'flex w-full items-center gap-3 rounded-md px-3 py-1.5 text-sm transition-colors',
                      active
                        ? 'bg-primary/10 text-primary font-medium'
                        : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                    )}
                  >
                    <item.icon className="h-3.5 w-3.5 shrink-0" />
                    {item.label}
                  </button>
                )
              })}
            </div>
          )}
        </div>
      </nav>

      {/* Runtime Status */}
      {runtime && (
        <div className="border-t border-border p-3 space-y-1.5">
          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Agents</span>
            <span className="font-medium text-foreground tabular-nums">
              {runtime.running_agents}/{runtime.total_agents}
            </span>
          </div>
          <div className="h-1.5 rounded-sm bg-muted overflow-hidden">
            <div
              className="h-full rounded-sm bg-primary transition-all duration-500"
              style={{
                width: `${runtime.total_agents > 0 ? (runtime.running_agents / runtime.total_agents) * 100 : 0}%`,
              }}
            />
          </div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Tokens</span>
            <span className="font-medium text-foreground tabular-nums">
              {(runtime.prompt_tokens + runtime.output_tokens).toLocaleString()}
            </span>
          </div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Phase</span>
            <span className="inline-flex items-center gap-1 font-medium text-foreground">
              <Circle
                className={cn(
                  'h-2 w-2 fill-current',
                  runtime.phase === 'processing' ? 'text-success' : 'text-muted-foreground'
                )}
              />
              {runtime.phase}
            </span>
          </div>
        </div>
      )}
    </aside>
  )
}
