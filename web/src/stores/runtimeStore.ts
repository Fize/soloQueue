import { create } from 'zustand'
import type { RuntimeStatus, SessionRuntimeState } from '@/types'

const DESIGN_MODE_KEY = 'soloqueue_design_mode'
const SIDEBAR_COLLAPSED_KEY = 'soloqueue_sidebar_collapsed'

function getStored(key: string, fallback: boolean): boolean {
  try {
    return localStorage.getItem(key) === 'true'
  } catch {
    return fallback
  }
}

function setStored(key: string, value: boolean) {
  try {
    localStorage.setItem(key, String(value))
  } catch {
    // ignore storage errors
  }
}

interface RuntimeState {
  status: RuntimeStatus | null
  setStatus: (status: RuntimeStatus | null) => void
  connectionStatus: 'connected' | 'disconnected' | 'reconnecting'
  setConnectionStatus: (status: 'connected' | 'disconnected' | 'reconnecting') => void
  sidebarCollapsed: boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  inspectorPanelWidth: number
  setInspectorPanelWidth: (w: number) => void
  isDesignMode: boolean
  setDesignMode: (active: boolean) => void
}

/** Resolve the logical session represented by an API runtime map entry. */
export function runtimeSessionId(
  key: string,
  runtime: Pick<SessionRuntimeState, 'session_id'>,
): string {
  if (runtime.session_id) return runtime.session_id
  if (key === 'l1' || key === 'l1-session' || key.startsWith('l1:')) return 'l1'
  return key
}

/** All runtime entries owned by one logical session, including legacy keys. */
export function runtimeRequestsForSession(status: RuntimeStatus | null | undefined, sessionId: string): SessionRuntimeState[] {
  if (!status?.sessions) return []
  return Object.entries(status.sessions)
    .filter(([key, runtime]) => runtimeSessionId(key, runtime) === sessionId)
    .map(([, runtime]) => runtime)
}

/** Resolve one request-owned runtime record without borrowing Agent globals. */
export function resolveRequestRuntime(
  status: RuntimeStatus | null | undefined,
  sessionId: string,
  requestId?: string,
): SessionRuntimeState | undefined {
  const entries = runtimeRequestsForSession(status, sessionId)
  if (requestId) {
    const exact = entries.find((entry) => entry.request_id === requestId)
    if (exact) return exact
  }
  const active = entries.filter((entry) => entry.state !== 'idle' && entry.state !== 'error')
  if (active.length > 0) return active[active.length - 1]
  return entries.find((entry) => entry.request_id === requestId) || entries[0]
}

export const useRuntimeStore = create<RuntimeState>((set) => ({
  status: null,
  setStatus: (status) => set({ status }),
  connectionStatus: 'disconnected',
  setConnectionStatus: (connectionStatus) => set({ connectionStatus }),
  sidebarCollapsed: getStored(SIDEBAR_COLLAPSED_KEY, false),
  setSidebarCollapsed: (sidebarCollapsed) => {
    setStored(SIDEBAR_COLLAPSED_KEY, sidebarCollapsed)
    set({ sidebarCollapsed })
  },
  inspectorPanelWidth: 0,
  setInspectorPanelWidth: (inspectorPanelWidth) => set({ inspectorPanelWidth }),
  isDesignMode: getStored(DESIGN_MODE_KEY, false),
  setDesignMode: (isDesignMode) => {
    setStored(DESIGN_MODE_KEY, isDesignMode)
    set({ isDesignMode })
  },
}))
