import { useEffect, useState, useRef, useCallback } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useRuntime } from '@/hooks/useRuntime'
import { Button } from '@/components/ui/button'
import {
  Bot,
  Kanban,
  FolderOpen,
  Settings,
  ChevronDown,
  ChevronRight,
  FileText,
  User,
  Sparkles,
  Server,
  Users,
  Circle,
  X,
  PanelLeftClose,
  PanelLeftOpen,
  LogOut,
} from 'lucide-react'
import { useAuthStore } from '@/stores/authStore'

const mainNav = [
  { to: '/', icon: Bot, label: 'Agents' },
  { to: '/plans', icon: Kanban, label: 'Plans' },
  { to: '/files', icon: FolderOpen, label: 'Files' },
]

const settingsChildren = [
  { to: '/settings/config', icon: FileText, label: 'Config' },
  { to: '/settings/profile', icon: User, label: 'Profile' },
  { to: '/settings/skills', icon: Sparkles, label: 'Skills' },
  { to: '/settings/mcp', icon: Server, label: 'MCP' },
  { to: '/settings/teams', icon: Users, label: 'Agents & Teams' },
]

function NavItem({
  to,
  icon: Icon,
  label,
  active,
  collapsed,
}: {
  to: string
  icon: typeof Bot
  label: string
  active: boolean
  collapsed?: boolean
}) {
  const navigate = useNavigate()
  return (
    <button
      onClick={() => navigate(to)}
      title={collapsed ? label : undefined}
      className={cn(
        'flex w-full items-center rounded-lg text-sm font-medium transition-all duration-200',
        collapsed ? 'justify-center px-2 py-2.5' : 'gap-3 px-3 py-2',
        active
          ? 'bg-primary/10 text-primary'
          : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
      )}
    >
      <Icon className={cn('shrink-0', collapsed ? 'h-5 w-5' : 'h-4 w-4')} />
      {!collapsed && <span className="truncate">{label}</span>}
    </button>
  )
}

export function Sidebar({ mobile, onClose }: { mobile?: boolean; onClose?: () => void }) {
  const location = useLocation()
  const navigate = useNavigate()
  const runtime = useRuntime()
  const { user, logout } = useAuthStore()

  const isSettingsActive = location.pathname.startsWith('/settings')
  const [settingsOpen, setSettingsOpen] = useState(isSettingsActive)
  const [collapsed, setCollapsed] = useState(false)

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

  const toggleCollapse = useCallback(() => {
    setCollapsed((c) => !c)
  }, [])

  const sidebarWidth = mobile ? 'w-[280px]' : collapsed ? 'w-14' : 'w-[240px]'

  return (
    <aside
      className={cn(
        'flex h-full shrink-0 flex-col border-r border-border bg-card transition-[width] duration-200',
        mobile ? 'fixed inset-y-0 left-0 z-50 w-[280px] shadow-2xl' : sidebarWidth
      )}
    >
      {/* Logo */}
      <div
        className={cn(
          'flex h-14 items-center border-b border-border shrink-0',
          collapsed && !mobile ? 'justify-center px-2' : 'gap-2.5 px-4'
        )}
      >
        {mobile && (
          <Button variant="ghost" size="icon" onClick={onClose} className="mr-1 shrink-0 h-8 w-8">
            <X className="h-4 w-4" />
          </Button>
        )}
        <Link to="/" className="flex items-center gap-2.5 min-w-0">
          <img src="/logo.png" alt="SoloQueue" className="h-7 w-7 shrink-0" />
          {(!collapsed || mobile) && (
            <span className="text-base font-bold tracking-tight text-foreground truncate">
              SoloQueue
            </span>
          )}
        </Link>
      </div>

      {/* Navigation */}
      <nav
        className={cn('flex-1 overflow-y-auto space-y-0.5', collapsed && !mobile ? 'p-1.5' : 'p-3')}
      >
        {mainNav.map((item) => (
          <NavItem
            key={item.to}
            to={item.to}
            icon={item.icon}
            label={item.label}
            collapsed={collapsed && !mobile}
            active={
              item.to === '/'
                ? location.pathname === '/' || location.pathname.startsWith('/agents')
                : location.pathname.startsWith(item.to)
            }
          />
        ))}

        {/* Settings with submenu */}
        <div className="pt-2">
          <button
            onClick={() => {
              if (collapsed && !mobile) {
                navigate('/settings/config')
              } else {
                setSettingsOpen(!settingsOpen)
              }
            }}
            title={collapsed && !mobile ? 'Settings' : undefined}
            className={cn(
              'flex w-full items-center rounded-lg text-sm font-medium transition-all duration-200',
              collapsed && !mobile ? 'justify-center px-2 py-2.5' : 'gap-3 px-3 py-2',
              isSettingsActive
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
            )}
          >
            <Settings className={cn('shrink-0', collapsed && !mobile ? 'h-5 w-5' : 'h-4 w-4')} />
            {(!collapsed || mobile) && (
              <>
                <span className="flex-1 text-left truncate">Settings</span>
                {settingsOpen ? (
                  <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
                )}
              </>
            )}
          </button>

          {settingsOpen && (!collapsed || mobile) && (
            <div className="mt-0.5 space-y-0.5 pl-3">
              {settingsChildren.map((item) => {
                const active = location.pathname === item.to
                return (
                  <button
                    key={item.to}
                    onClick={() => navigate(item.to)}
                    className={cn(
                      'flex w-full items-center gap-3 rounded-lg px-3 py-1.5 text-sm transition-colors',
                      active
                        ? 'bg-primary/10 text-primary font-medium'
                        : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
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
      {runtime && (!collapsed || mobile) && (
        <div className="border-t border-border p-3 space-y-1.5">
          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Agents</span>
            <span className="font-medium text-foreground tabular-nums">
              {runtime.running_agents}/{runtime.total_agents}
            </span>
          </div>
          <div className="h-1.5 rounded-full bg-muted overflow-hidden">
            <div
              className="h-full rounded-full bg-primary transition-all duration-500"
              style={{
                width: `${runtime.total_agents > 0 ? (runtime.running_agents / runtime.total_agents) * 100 : 0}%`,
              }}
            />
          </div>

          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Window</span>
            <span
              className={cn(
                'font-medium tabular-nums',
                runtime.context_pct >= 90
                  ? 'text-destructive'
                  : runtime.context_pct >= 70
                    ? 'text-warning'
                    : 'text-success'
              )}
            >
              {runtime.context_pct}%
            </span>
          </div>
          <div className="h-1.5 rounded-full bg-muted overflow-hidden">
            <div
              className={cn(
                'h-full rounded-full transition-all duration-500',
                runtime.context_pct >= 90
                  ? 'bg-destructive'
                  : runtime.context_pct >= 70
                    ? 'bg-warning'
                    : 'bg-success'
              )}
              style={{ width: `${Math.min(runtime.context_pct, 100)}%` }}
            />
          </div>

          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Phase</span>
            <span className="inline-flex items-center gap-1 font-medium text-foreground">
              <Circle
                className={cn(
                  'h-2 w-2 fill-current',
                  runtime.phase === 'processing'
                    ? 'text-success'
                    : runtime.phase === 'stopping'
                      ? 'text-warning'
                      : 'text-muted-foreground'
                )}
              />
              {runtime.phase || 'idle'}
            </span>
          </div>

          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Iter</span>
            <span className="font-medium text-foreground tabular-nums">{runtime.current_iter}</span>
          </div>

          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">Delegations</span>
            <span className="font-medium text-foreground tabular-nums">
              {runtime.active_delegations}
            </span>
          </div>

          {runtime.total_errors > 0 && (
            <div className="flex items-center justify-between text-xs">
              <span className="text-muted-foreground">Errors</span>
              <span className="font-medium text-destructive tabular-nums">
                {runtime.total_errors}
              </span>
            </div>
          )}
        </div>
      )}

      {/* Collapse toggle (desktop) + User/Logout footer */}
      <div className="border-t border-border shrink-0">
        {(!collapsed || mobile) && (
          <div className="flex items-center justify-between px-3 py-2">
            <span className="text-xs text-muted-foreground truncate">{user}</span>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 shrink-0"
              title="Logout"
              onClick={logout}
            >
              <LogOut className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}
        {!mobile && (
          <button
            onClick={toggleCollapse}
            className={cn(
              'flex w-full items-center justify-center py-2 text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors',
              collapsed && 'py-2.5'
            )}
            title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          >
            {collapsed ? (
              <PanelLeftOpen className="h-4 w-4" />
            ) : (
              <PanelLeftClose className="h-4 w-4" />
            )}
          </button>
        )}
      </div>
    </aside>
  )
}
