export type ThemeMode = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'soloqueue_theme'

/** Read persisted preference, defaulting to 'system'. */
export function getStoredTheme(): ThemeMode {
  if (typeof window === 'undefined') return 'system'
  return (localStorage.getItem(STORAGE_KEY) as ThemeMode) ?? 'system'
}

/** Persist a preference and apply it. */
export function setTheme(mode: ThemeMode): void {
  localStorage.setItem(STORAGE_KEY, mode)
  applyTheme(mode)
}

/** Cycle light → dark → system → light. */
export function cycleTheme(): ThemeMode {
  const current = getStoredTheme()
  const next: Record<ThemeMode, ThemeMode> = {
    light: 'dark',
    dark: 'system',
    system: 'light',
  }
  setTheme(next[current])
  return next[current]
}

/** Apply the given mode to the DOM. */
export function applyTheme(mode: ThemeMode): void {
  const isDark =
    mode === 'dark' ||
    (mode === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', isDark)
}

/** Listen for system preference changes (only meaningful in 'system' mode). */
export function listenSystemTheme(): () => void {
  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  const handler = () => {
    if (getStoredTheme() === 'system') {
      document.documentElement.classList.toggle('dark', mq.matches)
    }
  }
  mq.addEventListener('change', handler)
  return () => mq.removeEventListener('change', handler)
}
