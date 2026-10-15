import { create } from 'zustand'

export const INSPECTOR_STATE_STORAGE_KEY = 'soloqueue_inspector_state'
export const INSPECTOR_STATE_TTL_MS = 30 * 24 * 60 * 60 * 1000
export const INSPECTOR_STATE_MAX_SESSIONS = 100
export const INSPECTOR_EXPANDED_DIRECTORIES_MAX = 20
export const INSPECTOR_RELATIVE_PATH_MAX_LENGTH = 1024
export const INSPECTOR_PANEL_MIN_WIDTH = 200
export const INSPECTOR_PANEL_MAX_WIDTH = 1200

export type InspectorTab = 'files' | 'changes' | 'plan'

export interface InspectorSessionState {
  open: boolean
  tab: InspectorTab
  selectedPlan: string
  panelWidth: number
  projectPath: string
  expandedDirectories: string[]
  selectedFile: string | null
  lastActivity: number
}

interface StoredInspectorState {
  version: 1
  sessions: Record<string, InspectorSessionState>
}

interface InspectorStateStore {
  sessions: Record<string, InspectorSessionState>
  hydrate: () => void
  updateSession: (
    sessionId: string,
    patch: Partial<Omit<InspectorSessionState, 'lastActivity'>>,
  ) => void
  removeSession: (sessionId: string) => void
}

function pruneSessions(
  sessions: Record<string, InspectorSessionState>,
  now: number,
): Record<string, InspectorSessionState> {
  return Object.fromEntries(
    Object.entries(sessions)
      .filter(
        ([, snapshot]) =>
          snapshot.lastActivity <= now &&
          now - snapshot.lastActivity < INSPECTOR_STATE_TTL_MS,
      )
      .sort(([, left], [, right]) => right.lastActivity - left.lastActivity)
      .slice(0, INSPECTOR_STATE_MAX_SESSIONS),
  )
}

export function normalizeInspectorRelativePath(value: unknown): string | null {
  if (
    typeof value !== 'string' ||
    value === '' ||
    value.length > INSPECTOR_RELATIVE_PATH_MAX_LENGTH ||
    value.includes('\0')
  ) {
    return null
  }
  const slashPath = value.replace(/\\/g, '/')
  if (slashPath.startsWith('/') || /^[A-Za-z]:/.test(slashPath)) return null

  const normalizedSegments: string[] = []
  for (const segment of slashPath.split('/')) {
    if (segment === '' || segment === '.') continue
    if (segment === '..') return null
    normalizedSegments.push(segment)
  }
  return normalizedSegments.length > 0 ? normalizedSegments.join('/') : null
}

export function clampInspectorPanelWidth(width: number): number {
  return Math.max(INSPECTOR_PANEL_MIN_WIDTH, Math.min(width, INSPECTOR_PANEL_MAX_WIDTH))
}

function sanitizeInspectorSessionState(value: unknown): InspectorSessionState | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const snapshot = value as Record<string, unknown>
  if (
    typeof snapshot.open !== 'boolean' ||
    (snapshot.tab !== 'files' && snapshot.tab !== 'changes' && snapshot.tab !== 'plan') ||
    typeof snapshot.selectedPlan !== 'string' ||
    typeof snapshot.panelWidth !== 'number' ||
    !Number.isFinite(snapshot.panelWidth) ||
    typeof snapshot.projectPath !== 'string' ||
    !Array.isArray(snapshot.expandedDirectories) ||
    (snapshot.selectedFile !== null && typeof snapshot.selectedFile !== 'string') ||
    typeof snapshot.lastActivity !== 'number' ||
    !Number.isFinite(snapshot.lastActivity)
  ) {
    return null
  }

  const expandedDirectories = Array.from(
    new Set(
      snapshot.expandedDirectories
        .slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX)
        .map(normalizeInspectorRelativePath)
        .filter((path): path is string => path !== null),
    ),
  ).slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX)
  return {
    open: snapshot.open,
    tab: snapshot.tab,
    selectedPlan: snapshot.selectedPlan,
    panelWidth: clampInspectorPanelWidth(snapshot.panelWidth),
    projectPath: snapshot.projectPath,
    expandedDirectories,
    selectedFile: normalizeInspectorRelativePath(snapshot.selectedFile),
    lastActivity: snapshot.lastActivity,
  }
}

function createDefaultSessionState(now: number): InspectorSessionState {
  return {
    open: false,
    tab: 'files',
    selectedPlan: '',
    panelWidth: 300,
    projectPath: '',
    expandedDirectories: [],
    selectedFile: null,
    lastActivity: now,
  }
}

function persistSessions(sessions: Record<string, InspectorSessionState>) {
  const serialized = JSON.stringify({ version: 1, sessions })
  try {
    localStorage.setItem(INSPECTOR_STATE_STORAGE_KEY, serialized)
  } catch {
    try {
      // Removing stale storage may free quota; retry only once so UI writes never loop.
      localStorage.removeItem(INSPECTOR_STATE_STORAGE_KEY)
      localStorage.setItem(INSPECTOR_STATE_STORAGE_KEY, serialized)
    } catch {
      // Zustand already holds the update, so storage failure must not disable the Inspector.
    }
  }
}

export const useInspectorStateStore = create<InspectorStateStore>((set, get) => ({
  sessions: {},
  hydrate: () => {
    try {
      const raw = localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY)
      if (!raw) {
        set({ sessions: {} })
        return
      }
      const stored = JSON.parse(raw) as Partial<StoredInspectorState>
      if (
        stored.version !== 1 ||
        !stored.sessions ||
        typeof stored.sessions !== 'object' ||
        Array.isArray(stored.sessions)
      ) {
        localStorage.removeItem(INSPECTOR_STATE_STORAGE_KEY)
        set({ sessions: {} })
        return
      }
      const validSessions = Object.fromEntries(
        Object.entries(stored.sessions)
          .map(([sessionId, snapshot]) => [
            sessionId,
            sanitizeInspectorSessionState(snapshot),
          ] as const)
          .filter((entry): entry is readonly [string, InspectorSessionState] => entry[1] !== null),
      )
      const sessions = pruneSessions(validSessions, Date.now())
      set({ sessions })
      if (JSON.stringify(sessions) !== JSON.stringify(stored.sessions)) {
        persistSessions(sessions)
      }
    } catch {
      set({ sessions: {} })
      try {
        localStorage.removeItem(INSPECTOR_STATE_STORAGE_KEY)
      } catch {
        // Storage may be unavailable; the empty in-memory state remains usable.
      }
    }
  },
  updateSession: (sessionId, patch) => {
    const now = Date.now()
    const current = get().sessions[sessionId] ?? createDefaultSessionState(now)
    const sanitizedPatch = {
      ...patch,
      ...(patch.expandedDirectories !== undefined
        ? {
            expandedDirectories: Array.from(
              new Set(
                (Array.isArray(patch.expandedDirectories)
                  ? patch.expandedDirectories
                  : []
                )
                  .slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX)
                  .map(normalizeInspectorRelativePath)
                  .filter((path): path is string => path !== null),
              ),
            ).slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX),
          }
        : {}),
      ...(patch.selectedFile !== undefined
        ? { selectedFile: normalizeInspectorRelativePath(patch.selectedFile) }
        : {}),
      ...(patch.panelWidth !== undefined
        ? {
            panelWidth: Number.isFinite(patch.panelWidth)
              ? clampInspectorPanelWidth(patch.panelWidth)
              : current.panelWidth,
          }
        : {}),
    }
    const projectChanged =
      current.projectPath !== '' &&
      sanitizedPatch.projectPath !== undefined &&
      sanitizedPatch.projectPath !== current.projectPath
    const sessions = pruneSessions(
      {
        ...get().sessions,
        [sessionId]: {
          ...current,
          ...sanitizedPatch,
          ...(projectChanged ? { expandedDirectories: [], selectedFile: null } : {}),
          lastActivity: now,
        },
      },
      now,
    )
    set({ sessions })
    persistSessions(sessions)
  },
  removeSession: (sessionId) => {
    const remaining = { ...get().sessions }
    delete remaining[sessionId]
    const sessions = pruneSessions(remaining, Date.now())
    set({ sessions })
    persistSessions(sessions)
  },
}))

useInspectorStateStore.getState().hydrate()
