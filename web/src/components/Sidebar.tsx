import { useEffect, useState, useRef, useCallback } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Bot,
  FolderOpen,
  Settings,
  ChevronDown,
  ChevronRight,
  FileText,
  User,
  Sparkles,
  Server,
  Users,
  X,
  PanelLeftClose,
  PanelLeftOpen,
  Clock,
  Monitor,
  Sun,
  Moon,
  MessageSquare,
  Play,
} from 'lucide-react'
import { getStoredTheme, cycleTheme, type ThemeMode } from '@/lib/theme'
import { SessionTree } from './SessionTree'

const mainNav = [
  { to: '/files', icon: FolderOpen, label: 'Files' },
  { to: '/cron', icon: Clock, label: 'Cron Tasks' },
  { to: '/simulations', icon: Play, label: 'Simulations' },
]

const settingsChildren = [
  { to: '/settings/config', icon: FileText, label: 'Config' },
  { to: '/settings/profile', icon: User, label: 'Profile' },
  { to: '/settings/skills', icon: Sparkles, label: 'Skills' },
  { to: '/settings/mcp', icon: Server, label: 'MCP' },
  { to: '/settings/teams', icon: Users, label: 'Agents & Teams' },
  { to: '/settings/projects', icon: FolderOpen, label: 'Projects' },
  { to: '/settings/proxies', icon: Monitor, label: 'Proxies' },
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
  const [themeMode, setThemeMode] = useState<ThemeMode>(getStoredTheme())

  const isChatActive = location.pathname.startsWith('/chat')
  const [chatOpen, setChatOpen] = useState(isChatActive)
  const isSettingsActive = location.pathname.startsWith('/settings')
  const [settingsOpen, setSettingsOpen] = useState(isSettingsActive)
  const [collapsed, setCollapsed] = useState(false)
  const [proxies, setProxies] = useState<{ id: string }[]>([])

  useEffect(() => {
    if (isChatActive && !chatOpen) {
      setChatOpen(true)
    }
  }, [isChatActive])

  const fetchProxies = async () => {
    try {
      const res = await fetch('/api/proxy')
      if (res.ok) {
        const data = await res.json()
        setProxies(data || [])
      }
    } catch {
      // ignore
    }
  }

  useEffect(() => {
    fetchProxies()
    const handleProxyUpdated = () => fetchProxies()
    window.addEventListener('proxy-updated', handleProxyUpdated)
    return () => window.removeEventListener('proxy-updated', handleProxyUpdated)
  }, [])

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
          'relative flex h-14 items-center border-b border-border shrink-0',
          collapsed && !mobile ? 'justify-center px-2' : 'justify-center px-4'
        )}
      >
        {mobile && (
          <Button
            variant="ghost"
            size="icon"
            onClick={onClose}
            className="absolute left-3 shrink-0 h-8 w-8"
          >
            <X className="h-4 w-4" />
          </Button>
        )}
        <Link
          to="/"
          className={cn(
            'flex items-center gap-2.5 min-w-0',
            collapsed && !mobile ? '' : 'justify-center w-full'
          )}
        >
          <svg
            viewBox="0 0 32 32"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            className="h-7 w-7 shrink-0 text-primary"
          >
            {/* Squircle container matching Fluent large radius rx=7 */}
            <rect
              x="3"
              y="3"
              width="26"
              height="26"
              rx="7"
              stroke="url(#logo-gradient)"
              strokeWidth="2.5"
              fill="none"
            />
            {/* Connected S-Q Monogram */}
            {/* S path */}
            <path
              d="M12 11C12 9.5 13.5 8.5 15.5 8.5C17.5 8.5 19 9.5 19 11C19 12.5 17 13 15 13.5C13 14 11 14.5 11 16.5C11 18.5 12.5 19.5 15 19.5"
              stroke="url(#logo-gradient)"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            {/* Q loop and tail */}
            <circle cx="20.5" cy="15.5" r="4" stroke="url(#logo-gradient)" strokeWidth="2.5" />
            <path
              d="M22.5 18.5L25 21"
              stroke="url(#logo-gradient)"
              strokeWidth="2.5"
              strokeLinecap="round"
            />
            <defs>
              <linearGradient
                id="logo-gradient"
                x1="3"
                y1="3"
                x2="29"
                y2="29"
                gradientUnits="userSpaceOnUse"
              >
                <stop offset="0%" stopColor="var(--primary)" />
                <stop offset="100%" stopColor="var(--primary)" stopOpacity="0.75" />
              </linearGradient>
            </defs>
          </svg>
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
        {/* Chat Dropdown Menu */}
        <div className="py-0.5">
          <button
            onClick={() => {
              if (collapsed && !mobile) {
                navigate('/chat')
              } else {
                if (
                  !location.pathname.startsWith('/chat') &&
                  !location.pathname.startsWith('/agents')
                ) {
                  navigate('/chat')
                  setChatOpen(true)
                } else {
                  setChatOpen(!chatOpen)
                }
              }
            }}
            title={collapsed && !mobile ? 'Chat' : undefined}
            className={cn(
              'flex w-full items-center rounded-lg text-sm font-medium transition-all duration-200 cursor-pointer',
              collapsed && !mobile ? 'justify-center px-2 py-2.5' : 'gap-3 px-3 py-2',
              location.pathname.startsWith('/chat') || location.pathname.startsWith('/agents')
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
            )}
          >
            <MessageSquare
              className={cn('shrink-0', collapsed && !mobile ? 'h-5 w-5' : 'h-4 w-4')}
            />
            {(!collapsed || mobile) && (
              <>
                <span className="flex-1 text-left truncate">Chat</span>
                {chatOpen ? (
                  <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
                )}
              </>
            )}
          </button>

          {chatOpen && (!collapsed || mobile) && (
            <div className="mt-1 pl-1 pr-1">
              <SessionTree />
            </div>
          )}
        </div>

        {mainNav.map((item) => (
          <NavItem
            key={item.to}
            to={item.to}
            icon={item.icon}
            label={item.label}
            collapsed={collapsed && !mobile}
            active={location.pathname.startsWith(item.to)}
          />
        ))}

        {/* Dynamic Proxy/Iframe links */}
        {proxies.length > 0 && (
          <div className="pt-2 pb-1">
            <div
              className={cn(
                'text-xs font-semibold text-muted-foreground mb-1',
                collapsed && !mobile ? 'text-center' : 'px-3'
              )}
            >
              {collapsed && !mobile ? 'Apps' : 'Tools'}
            </div>
            <div className="space-y-0.5">
              {proxies.map((proxy) => (
                <NavItem
                  key={`proxy-${proxy.id}`}
                  to={`/iframe/${proxy.id}`}
                  icon={Monitor}
                  label={proxy.id}
                  collapsed={collapsed && !mobile}
                  active={location.pathname === `/iframe/${proxy.id}`}
                />
              ))}
            </div>
          </div>
        )}

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

      {/* Theme toggle + Collapse toggle (desktop) */}
      <div className="border-t border-border shrink-0">
        {!mobile && (
          <>
            {/* Theme toggle */}
            <button
              onClick={() => {
                const next = cycleTheme()
                setThemeMode(next)
              }}
              className={cn(
                'flex w-full items-center justify-center py-2 text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors',
                collapsed && 'py-2.5'
              )}
              title={
                themeMode === 'light'
                  ? 'Switch to dark mode'
                  : themeMode === 'dark'
                    ? 'Switch to system'
                    : 'Switch to light mode'
              }
            >
              {themeMode === 'light' ? (
                <Sun className={collapsed ? 'h-5 w-5' : 'h-4 w-4'} />
              ) : themeMode === 'dark' ? (
                <Moon className={collapsed ? 'h-5 w-5' : 'h-4 w-4'} />
              ) : (
                <Sun className={cn(collapsed ? 'h-5 w-5' : 'h-4 w-4', 'opacity-60')} />
              )}
            </button>
            {/* Collapse toggle */}
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
          </>
        )}
      </div>
    </aside>
  )
}
