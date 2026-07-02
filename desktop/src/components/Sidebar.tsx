import { useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import {
  FolderOpen,
  Settings,
  ChevronDown,
  ChevronRight,
  FileText,
  User,
  Sparkles,
  Server,
  Users,
  Clock,
  Sun,
  Moon,
  MessageSquare,
  Play,
  Gamepad2,
  ArrowLeft,
  Bot,
} from 'lucide-react'
import { getStoredTheme, cycleTheme, type ThemeMode } from '@/lib/theme'
import { SessionTree } from './SessionTree'

const mainNav = [
  { to: '/office', icon: Gamepad2, label: 'Office' },
  { to: '/assistant', icon: Bot, label: 'Assistant' },
  { to: '/simulations', icon: Play, label: 'Simulations' },
  { to: '/cron', icon: Clock, label: 'Scheduled Tasks' },
]

const settingsChildren = [
  { to: '/settings/config', icon: FileText, label: 'Configuration' },
  { to: '/settings/profile', icon: User, label: 'Profile' },
  { to: '/settings/skills', icon: Sparkles, label: 'Agent Skills' },
  { to: '/settings/mcp', icon: Server, label: 'MCP Services' },
  { to: '/settings/teams', icon: Users, label: 'Team Management' },
  { to: '/settings/projects', icon: FolderOpen, label: 'Projects' },
]

interface SidebarProps {
  /** True when sidebar should render narrow (collapsed & not hovered). */
  narrow: boolean
  /** True when sidebar should float-expand over content (collapsed & hovered). */
  floating: boolean
}

export function Sidebar({ narrow, floating }: SidebarProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const [themeMode, setThemeMode] = useState<ThemeMode>(getStoredTheme())
  const [chatOpen, setChatOpen] = useState(location.pathname.startsWith('/chat/l2:'))
  const [viewMode, setViewMode] = useState<'nav' | 'settings'>(
    location.pathname.startsWith('/settings') ? 'settings' : 'nav'
  )

  useEffect(() => {
    const onChatRoute = location.pathname.startsWith('/chat') || location.pathname.startsWith('/agents')
    setChatOpen(onChatRoute)
    if (location.pathname.startsWith('/settings')) {
      setViewMode('settings')
    } else {
      setViewMode('nav')
    }
  }, [location.pathname])

  const handleNav = (to: string) => {
    navigate(to)
  }

  return (
    <aside
      className={cn(
        'flex h-full flex-col bg-card/65 backdrop-blur-xl select-none overflow-hidden transition-transform duration-300 ease-out w-[220px]',
        (floating || narrow) ? 'absolute left-0 top-0 z-40 h-full' : 'h-full',
        floating && 'shadow-2xl border-r border-border/40',
        narrow && '-translate-x-full'
      )}
    >
      {/* Top spacer for macOS traffic lights */}
      <div className="h-12 w-full shrink-0 relative">
        {/* Traffic lights drag region (0 to 70px) */}
        <div className="absolute left-0 top-0 w-[70px] h-full electron-drag-region" />
        {/* Right side drag region (from 115px to end), only active when expanded */}
        {!narrow && (
          <div className="absolute left-[115px] right-0 top-0 h-full electron-drag-region" />
        )}
      </div>

      {viewMode === 'settings' ? (
        <SettingsView
          settingsChildren={settingsChildren}
          location={location}
          onNav={handleNav}
          onBack={() => setViewMode('nav')}
          narrow={false}
        />
      ) : (
        <NavView
          location={location}
          chatOpen={chatOpen}
          setChatOpen={setChatOpen}
          onNav={handleNav}
          narrow={false}
        />
      )}

      {/* Bottom fixed bar: settings icon (leftmost) + theme toggle. No version text. */}
      <div className="shrink-0 border-t border-border/30 flex items-center justify-start gap-1 px-2 py-2 bg-card/20">
        <button
          onClick={() => setViewMode('settings')}
          className={cn(
            'flex items-center justify-center rounded-md p-1.5 transition-colors duration-150',
            viewMode === 'settings'
              ? 'bg-foreground/10 text-foreground'
              : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
          )}
          title="Settings"
        >
          <Settings className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={() => {
            const next = cycleTheme()
            setThemeMode(next)
          }}
          className="flex items-center justify-center rounded-md p-1.5 text-muted-foreground hover:text-foreground hover:bg-foreground/5 transition-colors duration-150"
          title={
            themeMode === 'light'
              ? 'Switch to dark mode'
              : themeMode === 'dark'
                ? 'Switch to system theme'
                : 'Switch to light mode'
          }
        >
          {themeMode === 'light' ? (
            <Sun className="h-3.5 w-3.5" />
          ) : themeMode === 'dark' ? (
            <Moon className="h-3.5 w-3.5" />
          ) : (
            <div className="relative">
              <Sun className="h-3.5 w-3.5 opacity-40" />
              <Moon className="h-2 w-2 absolute -top-0.5 -right-0.5" />
            </div>
          )}
        </button>
      </div>
    </aside>
  )
}

/* ---------- Nav mode (default sidebar content) ---------- */

function NavView({
  location,
  chatOpen,
  setChatOpen,
  onNav,
  narrow,
}: {
  location: ReturnType<typeof useLocation>
  chatOpen: boolean
  setChatOpen: (v: boolean) => void
  onNav: (to: string) => void
  narrow: boolean
}) {
  const showText = !narrow
  return (
    <>
      {/* Navigation list */}
      <nav className="flex-1 overflow-y-auto overflow-x-hidden px-2 py-3 space-y-1">
        {/* Office + Assistant — first two nav items */}
        {mainNav.slice(0, 2).map((item) => {
          const active = location.pathname.startsWith(item.to)
          return (
            <div key={item.to}>
              <button
                onClick={() => onNav(item.to)}
                className={cn(
                  'flex items-center rounded-md text-xs font-medium transition-all duration-150 cursor-pointer',
                  narrow ? 'w-full justify-center px-0 py-2' : 'w-full gap-2 px-2.5 py-1.5',
                  active
                    ? 'bg-primary text-white shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
                )}
                title={narrow ? item.label : undefined}
              >
                <item.icon className="h-3.5 w-3.5 shrink-0" />
                {showText && <span className="whitespace-nowrap">{item.label}</span>}
              </button>
            </div>
          )
        })}

        {/* Session tree — between Assistant and Simulations */}
        {showText && (
          <div className="space-y-0.5">
            <button
              onClick={() => {
                if (!location.pathname.startsWith('/chat') && !location.pathname.startsWith('/agents')) {
                  onNav('/chat')
                }
                setChatOpen(!chatOpen)
              }}
              className={cn(
                'flex items-center rounded-md text-xs font-medium transition-all duration-150 cursor-pointer w-full gap-2 px-2.5 py-1.5',
                location.pathname.startsWith('/chat') || location.pathname.startsWith('/agents')
                  ? 'bg-primary text-white shadow-sm font-semibold'
                  : chatOpen
                    ? 'text-foreground hover:bg-foreground/5'
                    : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
              )}
            >
              <MessageSquare className="h-3.5 w-3.5 shrink-0" />
              <span className="flex-1 text-left">Sessions</span>
              {chatOpen ? (
                <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
              ) : (
                <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
              )}
            </button>

            {chatOpen && (
              <div className="pr-1 py-1">
                <SessionTree />
              </div>
            )}
          </div>
        )}

        {/* Simulations + Scheduled Tasks — remaining nav items */}
        {mainNav.slice(2).map((item) => {
          const active = location.pathname.startsWith(item.to)
          return (
            <div key={item.to}>
              <button
                onClick={() => onNav(item.to)}
                className={cn(
                  'flex items-center rounded-md text-xs font-medium transition-all duration-150 cursor-pointer',
                  narrow ? 'w-full justify-center px-0 py-2' : 'w-full gap-2 px-2.5 py-1.5',
                  active
                    ? 'bg-primary text-white shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
                )}
                title={narrow ? item.label : undefined}
              >
                <item.icon className="h-3.5 w-3.5 shrink-0" />
                {showText && <span className="whitespace-nowrap">{item.label}</span>}
              </button>
            </div>
          )
        })}

      </nav>
    </>
  )
}

/* ---------- Settings mode (full settings sidebar) ---------- */

function SettingsView({
  settingsChildren,
  location,
  onNav,
  onBack,
  narrow,
}: {
  settingsChildren: { to: string; icon: typeof FileText; label: string }[]
  location: ReturnType<typeof useLocation>
  onNav: (to: string) => void
  onBack: () => void
  narrow: boolean
}) {
  const showText = !narrow
  return (
    <>
      {/* Header: gray "back to app" button */}
      <div
        className={cn(
          'shrink-0 border-b border-border/30 transition-all duration-300',
          narrow ? 'px-1 py-2' : 'px-2 py-2'
        )}
      >
        <button
          onClick={onBack}
          className={cn(
            'flex items-center rounded-md text-xs font-medium transition-all duration-150 cursor-pointer text-muted-foreground hover:text-foreground hover:bg-foreground/5',
            narrow ? 'w-full justify-center px-0 py-2' : 'w-full gap-2 px-2.5 py-1.5'
          )}
          title={narrow ? 'Back to App' : undefined}
        >
          <ArrowLeft className="h-3.5 w-3.5 shrink-0" />
          {showText && <span className="whitespace-nowrap">Back to App</span>}
        </button>
      </div>

      {/* Settings items */}
      <nav className="flex-1 overflow-y-auto overflow-x-hidden px-2 py-3 space-y-1">
        {settingsChildren.map((item) => {
          const active = location.pathname === item.to
          return (
            <button
              key={item.to}
              onClick={() => onNav(item.to)}
              className={cn(
                'flex items-center rounded-md text-xs font-medium transition-all duration-150 cursor-pointer',
                narrow ? 'w-full justify-center px-0 py-2' : 'w-full gap-2.5 px-2.5 py-1.5',
                active
                  ? 'bg-primary text-white shadow-sm font-semibold'
                  : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
              )}
              title={narrow ? item.label : undefined}
            >
              <item.icon className="h-3.5 w-3.5 shrink-0" />
              {showText && <span className="whitespace-nowrap">{item.label}</span>}
            </button>
          )
        })}
      </nav>
    </>
  )
}