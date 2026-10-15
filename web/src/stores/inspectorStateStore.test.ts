import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  INSPECTOR_EXPANDED_DIRECTORIES_MAX,
  INSPECTOR_STATE_MAX_SESSIONS,
  INSPECTOR_PANEL_MAX_WIDTH,
  INSPECTOR_PANEL_MIN_WIDTH,
  INSPECTOR_STATE_STORAGE_KEY,
  INSPECTOR_STATE_TTL_MS,
  INSPECTOR_RELATIVE_PATH_MAX_LENGTH,
  useInspectorStateStore,
} from './inspectorStateStore'

beforeEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
  vi.restoreAllMocks()
  useInspectorStateStore.setState({ sessions: {} })
})

describe('inspectorStateStore', () => {
  it('hydrates independent valid session snapshots', () => {
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          'l2:one': {
            open: true,
            tab: 'plan',
            selectedPlan: 'plans/one.md',
            panelWidth: 420,
            projectPath: '/work/one',
            expandedDirectories: ['src'],
            selectedFile: 'src/one.ts',
            lastActivity: Date.now(),
          },
          'l2:two': {
            open: false,
            tab: 'changes',
            selectedPlan: '',
            panelWidth: 360,
            projectPath: '/work/two',
            expandedDirectories: [],
            selectedFile: null,
            lastActivity: Date.now(),
          },
        },
      }),
    )

    useInspectorStateStore.getState().hydrate()

    expect(useInspectorStateStore.getState().sessions).toMatchObject({
      'l2:one': { tab: 'plan', panelWidth: 420, selectedFile: 'src/one.ts' },
      'l2:two': { tab: 'changes', panelWidth: 360, selectedFile: null },
    })
  })

  it('clamps persisted panel widths to the usable range during hydration', () => {
    const session = (panelWidth: number) => ({
      open: true,
      tab: 'files',
      selectedPlan: '',
      panelWidth,
      projectPath: '/project',
      expandedDirectories: [],
      selectedFile: null,
      lastActivity: Date.now(),
    })
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          negative: session(-10),
          zero: session(0),
          huge: session(100_000),
        },
      }),
    )

    useInspectorStateStore.getState().hydrate()

    const sessions = useInspectorStateStore.getState().sessions
    expect(sessions.negative?.panelWidth).toBe(INSPECTOR_PANEL_MIN_WIDTH)
    expect(sessions.zero?.panelWidth).toBe(INSPECTOR_PANEL_MIN_WIDTH)
    expect(sessions.huge?.panelWidth).toBe(INSPECTOR_PANEL_MAX_WIDTH)
  })

  it('clears malformed storage without crashing hydration', () => {
    localStorage.setItem(INSPECTOR_STATE_STORAGE_KEY, '{invalid')

    expect(() => useInspectorStateStore.getState().hydrate()).not.toThrow()
    expect(useInspectorStateStore.getState().sessions).toEqual({})
    expect(localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY)).toBeNull()
  })

  it('clears storage written with an incompatible version', () => {
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({ version: 2, sessions: { stale: {} } }),
    )

    useInspectorStateStore.getState().hydrate()

    expect(useInspectorStateStore.getState().sessions).toEqual({})
    expect(localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY)).toBeNull()
  })

  it('prunes expired sessions before capping storage to the newest sessions', () => {
    const now = new Date('2026-10-15T00:00:00Z').getTime()
    vi.spyOn(Date, 'now').mockReturnValue(now)
    const sessions = Object.fromEntries([
      [
        'expired',
        {
          open: true,
          tab: 'files',
          selectedPlan: '',
          panelWidth: 300,
          projectPath: '/expired',
          expandedDirectories: [],
          selectedFile: null,
          lastActivity: now - INSPECTOR_STATE_TTL_MS - 1,
        },
      ],
      ...Array.from({ length: INSPECTOR_STATE_MAX_SESSIONS + 1 }, (_, index) => [
        `fresh-${index}`,
        {
          open: true,
          tab: 'files',
          selectedPlan: '',
          panelWidth: 300,
          projectPath: `/fresh/${index}`,
          expandedDirectories: [],
          selectedFile: null,
          lastActivity: now - index,
        },
      ]),
    ])
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({ version: 1, sessions }),
    )

    useInspectorStateStore.getState().hydrate()

    const hydrated = useInspectorStateStore.getState().sessions
    expect(Object.keys(hydrated)).toHaveLength(INSPECTOR_STATE_MAX_SESSIONS)
    expect(hydrated.expired).toBeUndefined()
    expect(hydrated['fresh-100']).toBeUndefined()
    expect(hydrated['fresh-0']).toBeDefined()
    expect(JSON.parse(localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY) || '{}').sessions).toEqual(
      hydrated,
    )
  })

  it('expires a session at the exact inactivity TTL boundary', () => {
    const now = new Date('2026-10-15T00:00:00Z').getTime()
    vi.spyOn(Date, 'now').mockReturnValue(now)
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          boundary: {
            open: true,
            tab: 'files',
            selectedPlan: '',
            panelWidth: 300,
            projectPath: '/boundary',
            expandedDirectories: [],
            selectedFile: null,
            lastActivity: now - INSPECTOR_STATE_TTL_MS,
          },
        },
      }),
    )

    useInspectorStateStore.getState().hydrate()

    expect(useInspectorStateStore.getState().sessions.boundary).toBeUndefined()
  })

  it('rejects future activity timestamps instead of letting them evade TTL cleanup', () => {
    const now = new Date('2026-10-15T00:00:00Z').getTime()
    vi.spyOn(Date, 'now').mockReturnValue(now)
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          future: {
            open: true,
            tab: 'files',
            selectedPlan: '',
            panelWidth: 300,
            projectPath: '/future',
            expandedDirectories: [],
            selectedFile: null,
            lastActivity: now + 1,
          },
        },
      }),
    )

    useInspectorStateStore.getState().hydrate()

    expect(useInspectorStateStore.getState().sessions.future).toBeUndefined()
  })

  it('updates one session, refreshes activity, and removes expired records on write', () => {
    const now = new Date('2026-10-15T00:00:00Z').getTime()
    vi.spyOn(Date, 'now').mockReturnValue(now)
    useInspectorStateStore.setState({
      sessions: {
        expired: {
          open: true,
          tab: 'files',
          selectedPlan: '',
          panelWidth: 300,
          projectPath: '/expired',
          expandedDirectories: [],
          selectedFile: null,
          lastActivity: now - INSPECTOR_STATE_TTL_MS - 1,
        },
      },
    })

    useInspectorStateStore.getState().updateSession('l2:one', {
      open: true,
      tab: 'plan',
      projectPath: '/work/one',
    })

    const sessions = useInspectorStateStore.getState().sessions
    expect(sessions.expired).toBeUndefined()
    expect(sessions['l2:one']).toEqual({
      open: true,
      tab: 'plan',
      selectedPlan: '',
      panelWidth: 300,
      projectPath: '/work/one',
      expandedDirectories: [],
      selectedFile: null,
      lastActivity: now,
    })
    expect(JSON.parse(localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY) || '{}')).toEqual({
      version: 1,
      sessions,
    })
  })

  it('clamps panel widths before updating persisted state', () => {
    useInspectorStateStore.getState().updateSession('l2:width', { panelWidth: -1 })
    expect(useInspectorStateStore.getState().sessions['l2:width']?.panelWidth).toBe(
      INSPECTOR_PANEL_MIN_WIDTH,
    )

    useInspectorStateStore.getState().updateSession('l2:width', { panelWidth: 50_000 })
    expect(useInspectorStateStore.getState().sessions['l2:width']?.panelWidth).toBe(
      INSPECTOR_PANEL_MAX_WIDTH,
    )
  })

  it('keeps in-memory updates and retries storage once when writes fail', () => {
    const setItem = vi.fn(() => {
      throw new DOMException('Quota exceeded', 'QuotaExceededError')
    })
    const removeItem = vi.fn()
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => null),
      setItem,
      removeItem,
      clear: vi.fn(),
    })

    expect(() =>
      useInspectorStateStore.getState().updateSession('l2:quota', { open: true }),
    ).not.toThrow()

    expect(useInspectorStateStore.getState().sessions['l2:quota']?.open).toBe(true)
    expect(setItem).toHaveBeenCalledTimes(2)
    expect(removeItem).toHaveBeenCalledTimes(1)
  })

  it('invalidates persisted file state when the project path changes', () => {
    useInspectorStateStore.getState().updateSession('l2:project', {
      open: true,
      tab: 'files',
      projectPath: '/work/old',
      expandedDirectories: ['src'],
      selectedFile: 'src/old.ts',
    })

    useInspectorStateStore.getState().updateSession('l2:project', {
      projectPath: '/work/new',
    })

    expect(useInspectorStateStore.getState().sessions['l2:project']).toMatchObject({
      open: true,
      tab: 'files',
      projectPath: '/work/new',
      expandedDirectories: [],
      selectedFile: null,
    })
  })

  it('sanitizes file paths before updating persisted state', () => {
    useInspectorStateStore.getState().updateSession('l2:paths', {
      projectPath: '/project',
      expandedDirectories: ['src//nested', '../outside', '/absolute', ''],
      selectedFile: 'src\\file.ts',
    })

    expect(useInspectorStateStore.getState().sessions['l2:paths']).toMatchObject({
      expandedDirectories: ['src/nested'],
      selectedFile: 'src/file.ts',
    })

    useInspectorStateStore.getState().updateSession('l2:paths', {
      selectedFile: 'C:\\outside.txt',
    })

    expect(useInspectorStateStore.getState().sessions['l2:paths']?.selectedFile).toBeNull()
  })

  it('bounds expanded directory count and relative path length on updates', () => {
    const directories = Array.from(
      { length: INSPECTOR_EXPANDED_DIRECTORIES_MAX + 5 },
      (_, index) => `src/${index}`,
    )

    useInspectorStateStore.getState().updateSession('l2:bounded', {
      expandedDirectories: directories,
      selectedFile: 'x'.repeat(INSPECTOR_RELATIVE_PATH_MAX_LENGTH + 1),
    })

    const snapshot = useInspectorStateStore.getState().sessions['l2:bounded']
    expect(snapshot?.expandedDirectories).toEqual(
      directories.slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX),
    )
    expect(snapshot?.selectedFile).toBeNull()

    useInspectorStateStore.getState().updateSession('l2:path-bounded', {
      expandedDirectories: [
        'x'.repeat(INSPECTOR_RELATIVE_PATH_MAX_LENGTH + 1),
        'src',
      ],
    })
    expect(
      useInspectorStateStore.getState().sessions['l2:path-bounded']?.expandedDirectories,
    ).toEqual(['src'])
  })

  it('bounds expanded directory count and relative path length during hydration', () => {
    const directories = Array.from(
      { length: INSPECTOR_EXPANDED_DIRECTORIES_MAX + 5 },
      (_, index) => `src/${index}`,
    )
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          bounded: {
            open: true,
            tab: 'files',
            selectedPlan: '',
            panelWidth: 300,
            projectPath: '/project',
            expandedDirectories: directories,
            selectedFile: 'x'.repeat(INSPECTOR_RELATIVE_PATH_MAX_LENGTH + 1),
            lastActivity: Date.now(),
          },
          'long-path': {
            open: true,
            tab: 'files',
            selectedPlan: '',
            panelWidth: 300,
            projectPath: '/project',
            expandedDirectories: [
              'x'.repeat(INSPECTOR_RELATIVE_PATH_MAX_LENGTH + 1),
              'src',
            ],
            selectedFile: null,
            lastActivity: Date.now(),
          },
        },
      }),
    )

    useInspectorStateStore.getState().hydrate()

    const snapshot = useInspectorStateStore.getState().sessions.bounded
    expect(snapshot?.expandedDirectories).toEqual(
      directories.slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX),
    )
    expect(snapshot?.selectedFile).toBeNull()
    expect(
      useInspectorStateStore.getState().sessions['long-path']?.expandedDirectories,
    ).toEqual(['src'])
  })

  it('removes one session from memory and persisted storage', () => {
    useInspectorStateStore.getState().updateSession('l2:remove', { open: true })
    useInspectorStateStore.getState().updateSession('l2:keep', { open: true })

    useInspectorStateStore.getState().removeSession('l2:remove')

    expect(useInspectorStateStore.getState().sessions['l2:remove']).toBeUndefined()
    expect(useInspectorStateStore.getState().sessions['l2:keep']).toBeDefined()
    expect(
      JSON.parse(localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY) || '{}').sessions,
    ).toEqual(useInspectorStateStore.getState().sessions)
  })

  it('hydrates persisted sessions when the store module starts', async () => {
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          'l2:startup': {
            open: true,
            tab: 'changes',
            selectedPlan: '',
            panelWidth: 390,
            projectPath: '/startup',
            expandedDirectories: [],
            selectedFile: null,
            lastActivity: Date.now(),
          },
        },
      }),
    )
    vi.resetModules()

    const { useInspectorStateStore: freshStore } = await import('./inspectorStateStore')

    expect(freshStore.getState().sessions['l2:startup']).toMatchObject({
      open: true,
      tab: 'changes',
      panelWidth: 390,
    })
  })

  it('filters structurally invalid session snapshots during hydration', () => {
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          invalid: {
            open: 'yes',
            tab: 'unknown',
            panelWidth: 'wide',
            expandedDirectories: 'src',
            lastActivity: Date.now(),
          },
        },
      }),
    )

    useInspectorStateStore.getState().hydrate()

    expect(useInspectorStateStore.getState().sessions).toEqual({})
    expect(JSON.parse(localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY) || '{}')).toEqual({
      version: 1,
      sessions: {},
    })
  })

  it('sanitizes persisted file paths during hydration', () => {
    localStorage.setItem(
      INSPECTOR_STATE_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        sessions: {
          unsafe: {
            open: true,
            tab: 'files',
            selectedPlan: '',
            panelWidth: 300,
            projectPath: '/project',
            expandedDirectories: [
              'src',
              'src//nested',
              './docs',
              '../outside',
              'src/../../outside',
              '/absolute',
              'C:\\drive',
              '',
            ],
            selectedFile: '../../secret.txt',
            lastActivity: Date.now(),
          },
        },
      }),
    )

    useInspectorStateStore.getState().hydrate()

    expect(useInspectorStateStore.getState().sessions.unsafe).toMatchObject({
      expandedDirectories: ['src', 'src/nested', 'docs'],
      selectedFile: null,
    })
  })

  it('cleans expired records while removing a session', () => {
    const now = new Date('2026-10-15T00:00:00Z').getTime()
    vi.spyOn(Date, 'now').mockReturnValue(now)
    useInspectorStateStore.setState({
      sessions: {
        expired: {
          open: false,
          tab: 'files',
          selectedPlan: '',
          panelWidth: 300,
          projectPath: '',
          expandedDirectories: [],
          selectedFile: null,
          lastActivity: now - INSPECTOR_STATE_TTL_MS - 1,
        },
        remove: {
          open: true,
          tab: 'files',
          selectedPlan: '',
          panelWidth: 300,
          projectPath: '',
          expandedDirectories: [],
          selectedFile: null,
          lastActivity: now,
        },
      },
    })

    useInspectorStateStore.getState().removeSession('remove')

    expect(useInspectorStateStore.getState().sessions).toEqual({})
    expect(JSON.parse(localStorage.getItem(INSPECTOR_STATE_STORAGE_KEY) || '{}')).toEqual({
      version: 1,
      sessions: {},
    })
  })
})
