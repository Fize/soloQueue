import { useEffect, useState, useRef, useCallback, lazy, Suspense } from 'react'
import { HashRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PanelLeftClose, PanelRightOpen, Server } from 'lucide-react'
import { Sidebar } from '@/components/Sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Toaster } from 'sonner'
import { wsManager } from '@/lib/websocket'
import { useConnectionStore, type BackendStatus } from '@/stores/connectionStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useChatStore } from '@/stores/chatStore'
import { useAgentStore } from '@/stores/agentStore'

// Lazy-loaded route components — split into separate chunks for faster initial load
const ChatPage = lazy(() => import('@/components/ChatPage').then(m => ({ default: m.ChatPage })))
const AssistantPage = lazy(() => import('@/components/AssistantPage').then(m => ({ default: m.AssistantPage })))
const AgentDetailPage = lazy(() => import('@/components/AgentDetailPage').then(m => ({ default: m.AgentDetailPage })))
const CronPage = lazy(() => import('@/components/CronPage').then(m => ({ default: m.CronPage })))
const SimulationListPage = lazy(() => import('@/components/SimulationListPage').then(m => ({ default: m.SimulationListPage })))
const SimulationDetailPage = lazy(() => import('@/components/SimulationDetailPage').then(m => ({ default: m.SimulationDetailPage })))
const SettingsLayout = lazy(() => import('@/components/SettingsLayout').then(m => ({ default: m.SettingsLayout })))
const ConfigTab = lazy(() => import('@/components/settings/ConfigTab/index').then(m => ({ default: m.ConfigTab })))
const ProfileTab = lazy(() => import('@/components/settings/ProfileTab').then(m => ({ default: m.ProfileTab })))
const SkillsTab = lazy(() => import('@/components/settings/SkillsTab').then(m => ({ default: m.SkillsTab })))
const MCPTab = lazy(() => import('@/components/settings/MCPTab').then(m => ({ default: m.MCPTab })))
const TeamsTab = lazy(() => import('@/components/settings/TeamsTab').then(m => ({ default: m.default })))
const ProjectsTab = lazy(() => import('@/components/settings/ProjectsTab').then(m => ({ default: m.ProjectsTab })))
const ConnectionTab = lazy(() => import('@/components/settings/ConnectionTab').then(m => ({ default: m.ConnectionTab })))
const StatsTab = lazy(() => import('@/components/settings/StatsTab').then(m => ({ default: m.StatsTab })))
const GeneralTab = lazy(() => import('@/components/settings/GeneralTab').then(m => ({ default: m.GeneralTab })))
function RouteFallback() {
  return (
    <div className="flex h-full items-center justify-center bg-background">
      <div className="text-sm text-muted-foreground font-mono animate-pulse">Loading...</div>
    </div>
  )
}

const LAST_CHAT_ROUTE_KEY = 'soloqueue_last_chat_route'

function getLastChatRoute() {
  try {
    const route = localStorage.getItem(LAST_CHAT_ROUTE_KEY)
    if (route?.startsWith('/chat/')) return route
  } catch {}
  return '/chat'
}

// ─── Connection Status Bar ────────────────────────────────────────────────────

const isElectron = typeof window !== 'undefined' && !!(window as any).electronAPI

function ConnectionStatusBar() {
  const mode = useConnectionStore((s) => s.mode)
  const remoteUrl = useConnectionStore((s) => s.remoteUrl)
  const backendStatus = useConnectionStore((s) => s.backendStatus)
  const isChecking = useConnectionStore((s) => s.isChecking)
  const connectionError = useConnectionStore((s) => s.connectionError)
  const setBackendStatus = useConnectionStore((s) => s.setBackendStatus)
  const setIsChecking = useConnectionStore((s) => s.setIsChecking)
  const setConnectionError = useConnectionStore((s) => s.setConnectionError)

  useEffect(() => {
    if (!isElectron) {
      setIsChecking(false)
      return
    }
    setIsChecking(true)

    const ea = (window as any).electronAPI

    ea.getBackendStatus().then((s: BackendStatus) => {
      setBackendStatus(s)
      if (s.running) setIsChecking(false)
    })

    const unsub = ea.onBackendStatusChanged((s: BackendStatus) => {
      setBackendStatus(s)
      setConnectionError(null)
      if (s.running) setIsChecking(false)
    })

    const timeout = setTimeout(() => {
      setIsChecking(false)
      if (!backendStatus.running && mode === 'local') {
        setConnectionError('Backend did not start in time. Check Settings → Connection.')
      }
    }, 12000)

    return () => {
      unsub()
      clearTimeout(timeout)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (mode === 'remote') {
    const hasUrl = !!remoteUrl
    return (
      <div
        className="h-[4px] w-full shrink-0 transition-colors duration-500"
        style={{ backgroundColor: hasUrl ? 'var(--md-primary)' : 'var(--md-warning)' }}
      />
    )
  }

  if (isChecking && !backendStatus.running) {
    return (
      <div className="h-[4px] w-full shrink-0 overflow-hidden bg-muted">
        <div
          className="h-full animate-indeterminate-bar"
          style={{
            width: '60%',
            background: 'linear-gradient(90deg, var(--md-primary) 0%, color-mix(in srgb, var(--md-primary) 60%, var(--md-tertiary)) 100%)',
            borderRadius: '2px',
          }}
        />
      </div>
    )
  }

  if (backendStatus.running) {
    return null
  }

  if (connectionError) {
    return (
      <div className="h-[28px] w-full shrink-0 flex items-center gap-2 px-4 text-xs font-medium text-white"
        style={{ backgroundColor: 'var(--md-error)' }}
      >
        <Server className="h-3.5 w-3.5" />
        <span className="flex-1 truncate">{connectionError}</span>
        <button
          onClick={() => {
            const ea = (window as any).electronAPI
            setConnectionError(null)
            setIsChecking(true)
            ea?.startBackend?.()
          }}
          className="underline cursor-pointer hover:opacity-80 shrink-0"
        >
          Retry
        </button>
      </div>
    )
  }

  return null
}

// ─── App ──────────────────────────────────────────────────────────────────────

function App() {
  const location = useLocation()
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed)
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed)
  const inspectorPanelWidth = useRuntimeStore((s) => s.inspectorPanelWidth)
  const [isHovered, setIsHovered] = useState(false)
  const hoverTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const effectivelyCollapsed = sidebarCollapsed && !isHovered
  const floating = sidebarCollapsed && isHovered

  const handleHoverEnter = useCallback(() => {
    if (hoverTimeoutRef.current) {
      clearTimeout(hoverTimeoutRef.current)
      hoverTimeoutRef.current = null
    }
    if (sidebarCollapsed) setIsHovered(true)
  }, [sidebarCollapsed])

  const handleSidebarHoverEnter = useCallback(() => {
    if (hoverTimeoutRef.current) {
      clearTimeout(hoverTimeoutRef.current)
      hoverTimeoutRef.current = null
    }
  }, [])

  const handleHoverLeave = useCallback(() => {
    if (hoverTimeoutRef.current) clearTimeout(hoverTimeoutRef.current)
    hoverTimeoutRef.current = setTimeout(() => setIsHovered(false), 200)
  }, [])

  const toggleCollapse = useCallback(() => {
    setSidebarCollapsed(!sidebarCollapsed)
    setIsHovered(false)
  }, [sidebarCollapsed, setSidebarCollapsed])

  useEffect(() => {
    if (!location.pathname.startsWith('/chat/')) return
    try {
      localStorage.setItem(
        LAST_CHAT_ROUTE_KEY,
        `${location.pathname}${location.search}${location.hash}`,
      )
    } catch {}
  }, [location.pathname, location.search, location.hash])

  useEffect(() => {
    wsManager.connect()
    return () => {
      wsManager.disconnect()
    }
  }, [])

  useEffect(() => {
    let lastRefresh = 0
    const handleFocusOrVisible = () => {
      const now = Date.now()
      if (now - lastRefresh < 2000) return
      lastRefresh = now

      wsManager.connect()
      useChatStore.getState().loadSessions()
      useAgentStore.getState().fetchLiveAgents()

      const activeSessionId = useChatStore.getState().activeSessionId
      if (activeSessionId) {
        useChatStore.getState().loadHistory(activeSessionId)
      }
    }

    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        handleFocusOrVisible()
      }
    }

    window.addEventListener('focus', handleFocusOrVisible)
    document.addEventListener('visibilitychange', onVisible)

    return () => {
      window.removeEventListener('focus', handleFocusOrVisible)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])

  return (
    <TooltipProvider>
      <Toaster
        position="top-center"
        toastOptions={{
          className: 'text-sm font-medium bg-card border border-border text-foreground rounded-lg shadow-lg',
        }}
      />
      <div className="flex h-full w-full bg-background overflow-hidden relative">
        {/* Independent collapse toggle button: lives in its own fixed wrapper so it
             stays above all other drag regions. Button itself has electron-no-drag to stay clickable. */}
        <div className="absolute left-[70px] top-0 z-[100] h-12 w-[45px] flex items-center justify-center electron-no-drag">
          <button
            onClick={toggleCollapse}
            onMouseEnter={handleHoverEnter}
            onMouseLeave={handleHoverLeave}
            className="flex items-center justify-center rounded-md p-1.5 transition-colors duration-150 hover:bg-foreground/10 text-muted-foreground hover:text-foreground shrink-0 cursor-pointer"
            title={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          >
            {sidebarCollapsed ? (
              <PanelRightOpen className="h-4 w-4 pointer-events-none" />
            ) : (
              <PanelLeftClose className="h-4 w-4 pointer-events-none" />
            )}
          </button>
        </div>

        {/* Main layout: Sidebar (translucent, height 100vh) + Content Pane */}
        <div className="flex flex-1 min-h-0 overflow-hidden">
          {/* macOS Style Sidebar wrapper - hover zone triggers popup expand */}
          <div
            className={cn(
              'shrink-0 h-full border-r transition-all duration-300 ease-out relative',
              sidebarCollapsed
                ? 'w-0 border-transparent bg-transparent'
                : 'w-[220px] border-border/40 bg-card/40 backdrop-blur-md overflow-hidden'
            )}
            onMouseEnter={handleSidebarHoverEnter}
            onMouseLeave={handleHoverLeave}
          >
            <Sidebar narrow={effectivelyCollapsed} floating={floating} />
          </div>

          {/* Main content pane */}
          <main className="flex flex-1 flex-col min-w-0 overflow-hidden h-full bg-background relative">
            {/* Connection status bar — 4pt HIG progress indicator */}
            <ConnectionStatusBar />

            {/* Title Bar drag region overlay */}
            <div className="absolute top-0 left-0 right-0 h-12 z-50 pointer-events-none">
              {sidebarCollapsed ? (
                <>
                  <div className="absolute left-0 top-0 w-[70px] h-full electron-drag-region" />
                  <div
                    className="absolute left-[115px] top-0 h-full electron-drag-region"
                    style={{ right: inspectorPanelWidth }}
                  />
                </>
              ) : (
                <div
                  className="absolute left-0 top-0 h-full electron-drag-region"
                  style={{ right: inspectorPanelWidth }}
                />
              )}
            </div>

            {/* Routes */}
            <div className="flex-1 overflow-hidden h-full">
              <Suspense fallback={<RouteFallback />}>
              <Routes>
                <Route path="/" element={<Navigate to={getLastChatRoute()} replace />} />
                <Route path="/new-chat" element={<Navigate to="/chat" replace />} />
                <Route path="/assistant" element={<AssistantPage />} />
                <Route path="/chat/:sessionId?" element={<ChatPage />} />
                <Route path="/agents/:id" element={<AgentDetailPage />} />
                <Route path="/cron" element={<CronPage />} />
                <Route path="/simulations" element={<SimulationListPage />} />
                <Route path="/simulations/:id" element={<SimulationDetailPage />} />
                <Route path="/stats" element={
                  <div className="h-full w-full overflow-y-auto bg-background">
                    <div className="flex justify-center p-6 md:p-8">
                      <div className="w-full max-w-5xl space-y-6">
                        <StatsTab />
                      </div>
                    </div>
                  </div>
                } />
                <Route path="/settings" element={<SettingsLayout />}>
                  <Route index element={<Navigate to="general" replace />} />
                  <Route path="general" element={<GeneralTab />} />
                  <Route path="config" element={<ConfigTab />} />
                  <Route path="connection" element={<ConnectionTab />} />
                  <Route path="profile" element={<ProfileTab />} />
                  <Route path="skills" element={<SkillsTab />} />
                  <Route path="mcp" element={<MCPTab />} />
                  <Route path="teams" element={<TeamsTab />} />
                  <Route path="projects" element={<ProjectsTab />} />
                  <Route path="stats" element={<StatsTab />} />
                </Route>
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
              </Suspense>
            </div>
          </main>
        </div>
      </div>
    </TooltipProvider>
  )
}

export default function AppWithRouter() {
  return (
    <HashRouter>
      <App />
    </HashRouter>
  )
}
