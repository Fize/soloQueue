import { useEffect, useState, useRef, useCallback, lazy, Suspense } from 'react'
import { HashRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PanelLeftClose, PanelRightOpen, Server, Loader2 } from 'lucide-react'
import { Sidebar } from '@/components/Sidebar'
import { useUIStore } from '@/stores/uiStore'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Toaster, toast } from 'sonner'
import { wsManager } from '@/lib/websocket'
import { notificationManager } from '@/lib/notification'
import { useConnectionStore, type BackendStatus } from '@/stores/connectionStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useChatStore } from '@/stores/chatStore'
import { useAgentStore } from '@/stores/agentStore'

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
  import('@/components/workflow/WorkflowEditorPage').then((m) => ({ default: m.WorkflowEditorPage }))
)
const WorkflowRunDetailPage = lazy(() =>
  import('@/components/workflow/WorkflowRunDetailPage').then((m) => ({ default: m.WorkflowRunDetailPage }))
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

const LAST_CHAT_ROUTE_KEY = 'soloqueue_last_chat_route'

function getLastChatRoute() {
  try {
    const route = localStorage.getItem(LAST_CHAT_ROUTE_KEY)
    if (route?.startsWith('/chat/')) return route
  } catch {
    // localStorage may be unavailable (private mode, disabled storage).
    // Intentional silent fallback to '/chat'.
  }
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
  const connectionStatus = useRuntimeStore((s) => s.connectionStatus)

  // Aggregate token scalars for tray sync — subscribe individually so
  // unrelated runtime field changes (agent_streams, context_pct, etc.)
  // do not trigger a tray IPC round-trip.
  const promptTokens = useRuntimeStore((s) => s.status?.prompt_tokens ?? 0)
  const outputTokens = useRuntimeStore((s) => s.status?.output_tokens ?? 0)
  const cacheHitTokens = useRuntimeStore((s) => s.status?.cache_hit_tokens ?? 0)
  const cacheMissTokens = useRuntimeStore((s) => s.status?.cache_miss_tokens ?? 0)

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

    // Read latest values from the store inside the timeout instead of
    // closing over the initial render's `backendStatus`/`mode` — the
    // previous closure made this always see `{running: false}` and fire
    // the "did not start in time" toast even when the backend was up.
    const timeout = setTimeout(() => {
      setIsChecking(false)
      const { backendStatus: latest, mode: latestMode } = useConnectionStore.getState()
      if (!latest.running && latestMode === 'local') {
        setConnectionError('Backend did not start in time. Check Settings → Connection.')
      }
    }, 12000)

    return () => {
      unsub()
      clearTimeout(timeout)
    }
  }, [])

  // ── Tray sync: push connection state to macOS menu bar Tray ──
  useEffect(() => {
    if (!isElectron) return
    const ea = (window as any).electronAPI
    ea?.notifyTrayStatus?.({
      mode,
      remoteUrl,
      hasUrl: !!remoteUrl,
      backendRunning: backendStatus.running,
      uptime: backendStatus.uptime,
      isChecking,
      connectionError,
      promptTokens,
      outputTokens,
      cacheHitTokens,
      cacheMissTokens,
    })
  }, [mode, remoteUrl, backendStatus.running, backendStatus.uptime, isChecking, connectionError, promptTokens, outputTokens, cacheHitTokens, cacheMissTokens])

  // ── Electron mode: connection status is surfaced via the macOS menu bar Tray.
  //    See `tray:update-status` in main.js. The in-app bar would conflict with
  //    HIG (macOS has no in-window "top status bar" component) and was creating
  //    a 4px baseline misalignment with the sidebar's traffic-light spacer.
  //    We still fire a toast on errors so the user gets visible feedback.
  useEffect(() => {
    if (!isElectron) return
    if (!connectionError) return
    toast.error(connectionError, {
      id: 'connection-error',
      duration: Infinity,
      action: {
        label: 'Retry',
        onClick: () => {
          const ea = (window as any).electronAPI
          setConnectionError(null)
          setIsChecking(true)
          ea?.startBackend?.()
        },
      },
    })
    return () => {
      toast.dismiss('connection-error')
    }
  }, [connectionError, setConnectionError, setIsChecking])

  if (isElectron && backendStatus.running) return null

  if (mode === 'remote') {
    // Hide the bar when the remote WebSocket connection is healthy,
    // mirroring the local-mode behavior that hides when the backend is running.
    if (connectionStatus === 'connected') return null

    const hasUrl = !!remoteUrl
    return (
      <div
        className="h-[4px] w-full shrink-0 transition-colors duration-500"
        style={{ backgroundColor: hasUrl ? 'var(--color-signal)' : 'var(--color-warning)' }}
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
            background:
              'linear-gradient(90deg, var(--color-signal) 0%, color-mix(in srgb, var(--color-signal) 60%, var(--color-accent)) 100%)',
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
      <div
        className="h-[28px] w-full shrink-0 flex items-center gap-2 px-4 text-xs font-medium text-white"
        style={{ backgroundColor: 'var(--color-destructive)' }}
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
  const theme = useUIStore((s) => s.theme)
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
        `${location.pathname}${location.search}${location.hash}`
      )
    } catch {
      // localStorage may be unavailable; persistence is best-effort.
    }
  }, [location.pathname, location.search, location.hash])

  useEffect(() => {
    wsManager.connect()

    // Subscribe to desktop notifications from the backend.
    const unsubNotify = wsManager.subscribe('notification', (payload) => {
      notificationManager.show(payload)
    })

    return () => {
      unsubNotify()
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
      <App />
    </HashRouter>
  )
}
