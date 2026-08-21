import { useEffect, useRef, useCallback, lazy, Suspense } from 'react'
import { HashRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PanelRightOpen, Loader2 } from 'lucide-react'
import { Sidebar } from '@/components/Sidebar'
import { useUIStore } from '@/stores/uiStore'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Toaster } from 'sonner'
import { wsManager } from '@/lib/websocket'
import { notificationManager } from '@/lib/notification'
import { useConnectionStore } from '@/stores/connectionStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useChatStore } from '@/stores/chatStore'
import { useAgentStore } from '@/stores/agentStore'
import { useTranslation } from '@/lib/i18n'
import { AuthGate } from '@/components/AuthGate'

// Lazy-loaded route components — split into separate chunks for faster initial load
const ChatPage = lazy(() => import('@/components/ChatPage').then((m) => ({ default: m.ChatPage })))
const AssistantPage = lazy(() =>
  import('@/components/AssistantPage').then((m) => ({ default: m.AssistantPage }))
)
const AgentDetailPage = lazy(() =>
  import('@/components/AgentDetailPage').then((m) => ({ default: m.AgentDetailPage }))
)
const CronPage = lazy(() => import('@/components/CronPage').then((m) => ({ default: m.CronPage })))
const SimulationListPage = lazy(() =>
  import('@/components/SimulationListPage').then((m) => ({ default: m.SimulationListPage }))
)
const SimulationDetailPage = lazy(() =>
  import('@/components/SimulationDetailPage').then((m) => ({ default: m.SimulationDetailPage }))
)
const SettingsLayout = lazy(() =>
  import('@/components/SettingsLayout').then((m) => ({ default: m.SettingsLayout }))
)
const ModelsTab = lazy(() =>
  import('@/components/settings/ModelsTab').then((m) => ({ default: m.ModelsTab }))
)
const MemoryTab = lazy(() =>
  import('@/components/settings/MemoryTab').then((m) => ({ default: m.MemoryTab }))
)
const SafetyTab = lazy(() =>
  import('@/components/settings/SafetyTab').then((m) => ({ default: m.SafetyTab }))
)
const AgentsTab = lazy(() =>
  import('@/components/settings/AgentsTab').then((m) => ({ default: m.AgentsTab }))
)
const CapabilitiesTab = lazy(() =>
  import('@/components/settings/CapabilitiesTab').then((m) => ({ default: m.CapabilitiesTab }))
)
const ChannelsTab = lazy(() =>
  import('@/components/settings/ChannelsTab').then((m) => ({ default: m.ChannelsTab }))
)
const ProjectsTab = lazy(() =>
  import('@/components/settings/ProjectsTab').then((m) => ({ default: m.ProjectsTab }))
)
const ConnectionTab = lazy(() =>
  import('@/components/settings/ConnectionTab').then((m) => ({ default: m.ConnectionTab }))
)
const StatsTab = lazy(() =>
  import('@/components/settings/StatsTab').then((m) => ({ default: m.StatsTab }))
)
const GeneralTab = lazy(() =>
  import('@/components/settings/GeneralTab').then((m) => ({ default: m.GeneralTab }))
)
const WorkflowListPage = lazy(() =>
  import('@/components/workflow/WorkflowListPage').then((m) => ({ default: m.WorkflowListPage }))
)
const WorkflowEditorPage = lazy(() =>
  import('@/components/workflow/WorkflowEditorPage').then((m) => ({
    default: m.WorkflowEditorPage,
  }))
)
const WorkflowRunDetailPage = lazy(() =>
  import('@/components/workflow/WorkflowRunDetailPage').then((m) => ({
    default: m.WorkflowRunDetailPage,
  }))
)
function RouteFallback() {
  return (
    <div
      className="flex h-full items-center justify-center bg-background gap-2"
      role="status"
      aria-live="polite"
    >
      <Loader2 className="h-4 w-4 animate-spin text-signal" aria-hidden="true" />
      <span className="text-sm text-muted-foreground font-mono">Loading…</span>
    </div>
  )
}

const LAST_ROUTE_KEY = 'soloqueue_last_route'

function getLastRoute() {
  try {
    const route = localStorage.getItem(LAST_ROUTE_KEY)
    const knownPrefixes = [
      '/chat',
      '/assistant',
      '/agents/',
      '/cron',
      '/simulations',
      '/workflows',
      '/stats',
      '/settings',
    ]
    if (route && knownPrefixes.some((p) => route.startsWith(p))) return route
  } catch {
    // localStorage may be unavailable (private mode, disabled storage).
    // Intentional silent fallback to '/chat'.
  }
  return '/chat'
}

// The browser only reports backend connectivity; lifecycle belongs to the
// `serve`/`start` commands.
function ConnectionStatusBar() {
  const status = useRuntimeStore((s) => s.connectionStatus)
  if (status === 'connected') return null
  return <div className="h-[4px] w-full shrink-0 bg-warning" aria-label="Backend disconnected" />
}

// ─── App ──────────────────────────────────────────────────────────────────────

function App() {
  const location = useLocation()
  const theme = useUIStore((s) => s.theme)
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed)
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed)
  const { t } = useTranslation()

  const toggleCollapse = useCallback(() => {
    setSidebarCollapsed(!sidebarCollapsed)
  }, [sidebarCollapsed, setSidebarCollapsed])

  useEffect(() => {
    const onShortcut = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing = target?.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target?.tagName || '')
      if (!typing && (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'b') {
        event.preventDefault()
        toggleCollapse()
      }
    }
    window.addEventListener('keydown', onShortcut)
    return () => window.removeEventListener('keydown', onShortcut)
  }, [toggleCollapse])

  useEffect(() => {
    // Transient redirect routes (launch root, /new-chat) must never clobber
    // the stored route before the restore navigation lands.
    if (location.pathname === '/' || location.pathname === '/new-chat') return
    try {
      localStorage.setItem(LAST_ROUTE_KEY, `${location.pathname}${location.search}${location.hash}`)
    } catch {
      // localStorage may be unavailable; persistence is best-effort.
    }
  }, [location.pathname, location.search, location.hash])

  useEffect(() => {
    wsManager.connect()

    // Subscribe to notifications from the backend.
    const unsubNotify = wsManager.subscribe('notification', (payload) => {
      notificationManager.show(payload)
    })

    // Poll the backend health endpoint so the renderer-side readiness signal
    // stays accurate for the browser backend.
    const stopHealthPolling = useConnectionStore.getState().startBackendHealthPolling()

    return () => {
      unsubNotify()
      stopHealthPolling()
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

  // Auto-retry data loads when the backend transitions to running. We track
  // the previous value with a ref so we only fire on the false→true edge —
  // the initial mount (handled by SessionTree / ChatPage effects, which are
  // also dedup'd inside the stores) does not need a second trigger.
  const backendRunning = useConnectionStore((s) => s.backendStatus.running)
  const prevBackendRunningRef = useRef(false)
  useEffect(() => {
    const wasRunning = prevBackendRunningRef.current
    prevBackendRunningRef.current = backendRunning
    if (!backendRunning || wasRunning) return
    useChatStore.getState().loadSessions()
    useAgentStore.getState().fetchLiveAgents()
    useAgentStore.getState().fetchTeams()
  }, [backendRunning])

  // Reconnect the WebSocket when the health probe confirms the backend is
  // ready (false→true edge). connect() is idempotent — it returns early when
  // a socket is already OPEN or CONNECTING — so calling it repeatedly is safe.
  const backendReady = useConnectionStore((s) => s.backendReady)
  const prevBackendReadyRef = useRef(false)
  useEffect(() => {
    const wasReady = prevBackendReadyRef.current
    prevBackendReadyRef.current = backendReady
    if (!backendReady || wasReady) return
    wsManager.connect()
  }, [backendReady])

  return (
    <TooltipProvider>
      <Toaster
        theme={theme}
        position="top-center"
        toastOptions={{
          className:
            'text-sm font-medium bg-card border border-border text-foreground rounded-lg shadow-lg',
        }}
      />
      <div className="flex h-full w-full bg-background overflow-hidden relative">
        {sidebarCollapsed && (
          <button
            type="button"
            onClick={toggleCollapse}
            className="absolute left-3 top-3 z-[100] flex h-9 w-9 items-center justify-center rounded-xl border border-border/60 bg-card/85 text-muted-foreground shadow-lg backdrop-blur-md transition-all duration-150 hover:border-primary/40 hover:bg-card hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            title={t('sidebar.expand')}
            aria-label={t('sidebar.expand')}
          >
            <PanelRightOpen className="h-4 w-4" aria-hidden="true" />
          </button>
        )}

        {/* Main layout: Sidebar (translucent, height 100vh) + Content Pane */}
        <div className="flex flex-1 min-h-0 overflow-hidden">
          {/* Sidebar shell; collapsed state is restored by explicit click. */}
          <div
            className={cn(
              'shrink-0 h-full border-r transition-all duration-300 ease-out relative',
              sidebarCollapsed
                ? 'w-0 border-transparent bg-transparent'
                : 'w-[220px] border-border/40 bg-sidebar backdrop-blur-md overflow-hidden'
            )}
          >
            <Sidebar
              narrow={sidebarCollapsed}
              onToggleCollapse={toggleCollapse}
            />
          </div>

          {/* Main content pane */}
          <main className="flex flex-1 flex-col min-w-0 overflow-hidden h-full bg-background relative">
            {/* Connection status bar — 4pt HIG progress indicator */}
            <ConnectionStatusBar />

            {/* Routes */}
            <div className="flex-1 overflow-hidden h-full">
              <Suspense fallback={<RouteFallback />}>
                <Routes>
                  <Route path="/" element={<Navigate to={getLastRoute()} replace />} />
                  <Route path="/new-chat" element={<Navigate to="/chat" replace />} />
                  <Route path="/assistant" element={<AssistantPage />} />
                  <Route path="/chat/:sessionId?" element={<ChatPage />} />
                  <Route path="/agents/:id" element={<AgentDetailPage />} />
                  <Route path="/cron" element={<CronPage />} />
                  <Route path="/simulations" element={<SimulationListPage />} />
                  <Route path="/simulations/:id" element={<SimulationDetailPage />} />
                  <Route path="/workflows" element={<WorkflowListPage />} />
                  <Route path="/workflows/:name" element={<WorkflowEditorPage />} />
                  <Route path="/workflows/:name/runs/:runId" element={<WorkflowRunDetailPage />} />
                  <Route
                    path="/stats"
                    element={
                      <div className="h-full w-full overflow-y-auto bg-background">
                        <div className="flex justify-center p-6 md:p-8">
                          <div className="w-full max-w-5xl space-y-6">
                            <StatsTab />
                          </div>
                        </div>
                      </div>
                    }
                  />
                  <Route path="/settings" element={<SettingsLayout />}>
                    <Route index element={<Navigate to="general" replace />} />
                    <Route path="general" element={<GeneralTab />} />
                    <Route path="connection" element={<ConnectionTab />} />
                    <Route path="projects" element={<ProjectsTab />} />
                    <Route path="models" element={<ModelsTab />} />
                    <Route path="memory" element={<MemoryTab />} />
                    <Route path="safety" element={<SafetyTab />} />
                    <Route path="agents" element={<AgentsTab />} />
                    <Route path="capabilities" element={<CapabilitiesTab />} />
                    <Route path="channels" element={<ChannelsTab />} />
                    <Route path="qqbot" element={<Navigate to="../channels" replace />} />
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
      <AuthenticatedApp />
    </HashRouter>
  )
}

function AuthenticatedApp() {
  const authState = useConnectionStore((state) => state.authState)
  if (authState !== 'not_required' && authState !== 'authenticated') {
    return <AuthGate />
  }
  return <App />
}
