import { useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import {
  FolderOpen,
  Settings,
  ChevronDown,
  ChevronRight,
  User,
  Sparkles,
  Clock,
  Sun,
  Moon,
  MessageSquare,
  Play,
  ArrowLeft,
  Bot,
  Plus,
  Wifi,
  BarChart2,
  Cpu,
  Brain,
  Shield,
  Loader2,
  Workflow,
  PanelLeftClose,
} from 'lucide-react'
import { cycleTheme } from '@/lib/theme'
import { SessionTree } from './SessionTree'
import { useUIStore } from '@/stores/uiStore'
import { useChatStore } from '@/stores/chatStore'
import { useTranslation } from '@/lib/i18n'

const mainNav = [
  { to: '/simulations', icon: Play, key: 'sidebar.simulations' as const },
  { to: '/workflows', icon: Workflow, key: 'sidebar.workflows' as const },
  { to: '/cron', icon: Clock, key: 'sidebar.scheduledTasks' as const },
  { to: '/stats', icon: BarChart2, key: 'sidebar.usageStats' as const },
]

const systemSettings = [
  { to: '/settings/general', icon: Settings, key: 'sidebar.general' as const },
  { to: '/settings/connection', icon: Wifi, key: 'sidebar.connection' as const },
  { to: '/settings/projects', icon: FolderOpen, key: 'sidebar.projects' as const },
]

const engineSettings = [
  { to: '/settings/models', icon: Cpu, key: 'sidebar.models' as const },
  { to: '/settings/memory', icon: Brain, key: 'sidebar.memory' as const },
  { to: '/settings/safety', icon: Shield, key: 'sidebar.safety' as const },
]

const agentSettings = [
  { to: '/settings/agents', icon: User, key: 'sidebar.agents' as const },
  { to: '/settings/capabilities', icon: Sparkles, key: 'sidebar.capabilities' as const },
  { to: '/settings/channels', icon: MessageSquare, key: 'sidebar.channels' as const },
]

interface SidebarProps {
  /** True when sidebar is collapsed and hidden until explicitly expanded. */
  narrow: boolean
  onToggleCollapse: () => void
}

export function Sidebar({
  narrow,
  onToggleCollapse,
}: SidebarProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const { theme: themeMode, setTheme } = useUIStore()
  const sessionsLoading = useChatStore((s) => s.sessionsLoading)
  const { t } = useTranslation()
  const [chatOpen, setChatOpen] = useState(
    location.pathname.startsWith('/chat') || location.pathname === '/new-chat'
  )
  const [viewMode, setViewMode] = useState<'nav' | 'settings'>(
    location.pathname.startsWith('/settings') ? 'settings' : 'nav'
  )

  useEffect(() => {
    const onChatRoute = location.pathname.startsWith('/chat') || location.pathname === '/new-chat' || location.pathname.startsWith('/agents')
    // auto-expand when on a chat route, but never auto-collapse
    if (onChatRoute) setChatOpen(true)
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
        'flex h-full flex-col bg-surface-secondary backdrop-blur-xl select-none overflow-hidden transition-transform duration-300 ease-out w-[220px]',
        narrow ? 'absolute left-0 top-0 z-40 h-full' : 'h-full',
        narrow && '-translate-x-full'
      )}
    >
      <header className="flex h-14 shrink-0 items-center justify-between border-b border-border/35 bg-background/20 px-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-primary/30 bg-primary/15 text-primary shadow-[0_0_18px_rgba(35,99,234,.16)]"
            aria-hidden="true"
          >
            <span className="h-2 w-2 rounded-full bg-current shadow-[5px_-3px_0_-1px_currentColor,5px_3px_0_-1px_currentColor]" />
          </div>
          {!narrow && (
            <div className="min-w-0 leading-none">
              <div className="truncate text-sm font-semibold tracking-tight text-foreground">SoloQueue</div>
              <div className="mt-1 text-[9px] font-semibold uppercase tracking-[0.18em] text-muted-foreground/65">Workbench</div>
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={onToggleCollapse}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors duration-150 hover:bg-foreground/8 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          title={t('sidebar.collapse')}
          aria-label={t('sidebar.collapse')}
        >
          <PanelLeftClose className="h-4 w-4" aria-hidden="true" />
        </button>
      </header>

      {viewMode === 'settings' ? (
        <SettingsView
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
          sessionsLoading={sessionsLoading}
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
          title={t('sidebar.settings')}
        >
          <Settings className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={() => {
            const next = cycleTheme()
            setTheme(next)
          }}
          className="flex items-center justify-center rounded-md p-1.5 text-muted-foreground hover:text-foreground hover:bg-foreground/5 transition-colors duration-150"
          title={
            themeMode === 'light'
              ? t('sidebar.switchDark')
              : themeMode === 'dark'
                ? t('sidebar.switchSystem')
                : t('sidebar.switchLight')
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
  sessionsLoading,
}: {
  location: ReturnType<typeof useLocation>
  chatOpen: boolean
  setChatOpen: (v: boolean) => void
  onNav: (to: string) => void
  narrow: boolean
  sessionsLoading: boolean
}) {
  const showText = !narrow
  const { t } = useTranslation()
  return (
    <>
      {/* Navigation list */}
      <nav className="flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 space-y-1">
        {/* New Chat — top nav item, navigates to home. Active only on the
            bare /chat welcome screen; once inside a session (/chat/:id) the
            "会话" tree header takes the active state so the two
            buttons don't both highlight at once. */}
        {(() => {
          const item = { to: '/chat', icon: Plus, key: 'chat.newChat' as const }
          const active =
            location.pathname === '/chat' || location.pathname === '/chat/'
          return (
            <div key={item.to}>
              <button
                onClick={() => onNav(item.to)}
                className={cn(
                  'flex items-center rounded-xl text-xs font-medium transition-all duration-150 cursor-pointer',
                  narrow ? 'w-full justify-center px-0 py-2' : 'w-full gap-2 px-2.5 py-1.5',
                  active
                    ? 'bg-primary text-white shadow-[0_6px_18px_rgba(35,99,234,.22)] font-semibold'
                    : 'border border-primary/20 bg-primary/8 text-primary hover:bg-primary/15'
                )}
                title={narrow ? t(item.key) : undefined}
              >
                <item.icon className="h-3.5 w-3.5 shrink-0" />
                {showText && <span className="whitespace-nowrap">{t(item.key)}</span>}
              </button>
            </div>
          )
        })()}

        {/* Assistant — moved below New Chat, above the chat tree */}
        {(() => {
          const item = { to: '/assistant', icon: Bot, key: 'sidebar.assistant' as const };
          const active = location.pathname.startsWith(item.to);
          return (
            <div key={item.to}>
              <button
                onClick={() => onNav(item.to)}
                className={cn(
                  'flex items-center rounded-lg text-xs font-medium transition-all duration-150 cursor-pointer',
                  narrow ? 'w-full justify-center px-0 py-2' : 'w-full gap-2 px-2.5 py-1.5',
                  active
                    ? 'bg-primary text-white shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
                )}
                title={narrow ? t(item.key) : undefined}
              >
                <item.icon className="h-3.5 w-3.5 shrink-0" />
                {showText && <span className="whitespace-nowrap">{t(item.key)}</span>}
              </button>
            </div>
          );
        })()}

        {/* Chats tree — toggle only, no navigation. Active on /chat/:id
            (i.e. inside a session) and /agents/*; not active on bare /chat
            because that route belongs to the "New Chat" button above.
            When sessionsLoading is true we swap the chevron for a small
            spinner so the user gets immediate visual feedback that the
            data is on its way. */}
        {showText && (
          <div className="mt-3 space-y-0.5 border-t border-border/25 pt-3">
            <button
              onClick={() => setChatOpen(!chatOpen)}
              className={cn(
                'flex items-center rounded-lg text-xs font-medium transition-all duration-150 cursor-pointer w-full gap-2 px-2.5 py-1.5',
                (location.pathname.startsWith('/chat/') ||
                  location.pathname.startsWith('/agents'))
                  ? 'bg-primary text-white shadow-sm font-semibold'
                  : chatOpen
                    ? 'text-foreground hover:bg-foreground/5'
                    : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
              )}
            >
              <MessageSquare className="h-3.5 w-3.5 shrink-0" />
              <span className="flex-1 text-left">{t('chat.title')}</span>
              {sessionsLoading ? (
                <Loader2
                  className="h-3 w-3 shrink-0 text-muted-foreground animate-spin"
                  aria-label={t('sessionTree.loading')}
                  role="status"
                />
              ) : chatOpen ? (
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

        {/* All nav items: Simulations, Scheduled Tasks, Assistant */}
        {mainNav.map((item) => {
          const active = location.pathname.startsWith(item.to)
          return (
            <div key={item.to}>
              <button
                onClick={() => onNav(item.to)}
                className={cn(
                  'flex items-center rounded-lg text-xs font-medium transition-all duration-150 cursor-pointer',
                  narrow ? 'w-full justify-center px-0 py-2' : 'w-full gap-2 px-2.5 py-1.5',
                  active
                    ? 'bg-primary text-white shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
                )}
                title={narrow ? t(item.key) : undefined}
              >
                <item.icon className="h-3.5 w-3.5 shrink-0" />
                {showText && <span className="whitespace-nowrap">{t(item.key)}</span>}
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
  location,
  onNav,
  onBack,
  narrow,
}: {
  location: ReturnType<typeof useLocation>
  onNav: (to: string) => void
  onBack: () => void
  narrow: boolean
}) {
  const showText = !narrow
  const { t } = useTranslation()

  const renderSection = (titleKey: string, items: { to: string; icon: any; key: any }[]) => {
    return (
      <div className="space-y-1">
        {showText && (
          <div className="px-2.5 pt-1.5 pb-1 text-[10px] font-bold tracking-wider text-muted-foreground uppercase">
            {t(titleKey)}
          </div>
        )}
        {items.map((item) => {
          const active = location.pathname === item.to
          return (
            <button
              key={item.to}
              onClick={() => onNav(item.to)}
              className={cn(
                'flex items-center rounded-md text-xs font-medium transition-all duration-150 cursor-pointer w-full',
                narrow ? 'justify-center px-0 py-2' : 'gap-2.5 px-2.5 py-1.5',
                active
                  ? 'bg-primary text-white shadow-sm font-semibold'
                  : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
              )}
              title={narrow ? t(item.key) : undefined}
            >
              <item.icon className="h-3.5 w-3.5 shrink-0" />
              {showText && <span className="whitespace-nowrap">{t(item.key)}</span>}
            </button>
          )
        })}
      </div>
    )
  }

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
          title={narrow ? t('sidebar.backToApp') : undefined}
        >
          <ArrowLeft className="h-3.5 w-3.5 shrink-0" />
          {showText && <span className="whitespace-nowrap">{t('sidebar.backToApp')}</span>}
        </button>
      </div>

      {/* Settings items */}
      <nav className="flex-1 overflow-y-auto overflow-x-hidden px-2 py-3 space-y-4">
        {renderSection('sidebar.groupSystem', systemSettings)}
        {showText ? <div className="border-t border-border/10 my-1" /> : <div className="border-t border-border/30 my-1" />}
        {renderSection('sidebar.groupEngine', engineSettings)}
        {showText ? <div className="border-t border-border/10 my-1" /> : <div className="border-t border-border/30 my-1" />}
        {renderSection('sidebar.groupAgents', agentSettings)}
      </nav>
    </>
  )
}
