import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { Monitor, Moon, Sun } from 'lucide-react'

export type ThemeMode = 'system' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

interface ThemeContextValue {
  mode: ThemeMode
  theme: ResolvedTheme
  setMode: (mode: ThemeMode) => void
  cycleMode: () => void
}

const STORAGE_KEY = 'soloqueue-theme-mode'

const ThemeContext = createContext<ThemeContextValue>({
  mode: 'system',
  theme: 'dark',
  setMode: () => {},
  cycleMode: () => {},
})

function getSystemTheme(): ResolvedTheme {
  if (typeof window === 'undefined') return 'dark'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => {
    const stored = localStorage.getItem(STORAGE_KEY) || localStorage.getItem('soloqueue-theme')
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored as ThemeMode
    return 'system'
  })

  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>(getSystemTheme)

  // Listen for OS color scheme changes
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handleChange = (e: MediaQueryListEvent) => {
      setSystemTheme(e.matches ? 'dark' : 'light')
    }
    mq.addEventListener('change', handleChange)
    return () => mq.removeEventListener('change', handleChange)
  }, [])

  const theme: ResolvedTheme = mode === 'system' ? systemTheme : mode

  useEffect(() => {
    const root = document.documentElement
    // Architectural Decision: Apply .light class when resolved theme is light, .dark when dark.
    if (theme === 'light') {
      root.classList.add('light')
      root.classList.remove('dark')
    } else {
      root.classList.remove('light')
      root.classList.add('dark')
    }
    localStorage.setItem(STORAGE_KEY, mode)
  }, [mode, theme])

  const setMode = (newMode: ThemeMode) => {
    setModeState(newMode)
  }

  const cycleMode = () => {
    setModeState(prev => {
      if (prev === 'system') return 'light'
      if (prev === 'light') return 'dark'
      return 'system'
    })
  }

  return (
    <ThemeContext.Provider value={{ mode, theme, setMode, cycleMode }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  return useContext(ThemeContext)
}

export function ThemeToggle() {
  const { mode, cycleMode } = useTheme()

  const labels: Record<ThemeMode, string> = {
    system: 'Theme: System (Auto)',
    light: 'Theme: Light',
    dark: 'Theme: Dark',
  }

  return (
    <button
      onClick={cycleMode}
      title={labels[mode]}
      aria-label={labels[mode]}
      className="relative flex items-center justify-center w-9 h-9 rounded-full
        bg-[var(--color-surface-secondary)] hover:brightness-110
        border border-[var(--color-border)] cursor-pointer
        transition-all duration-200 active:scale-95 shrink-0
        focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-primary)]"
    >
      <Monitor
        className={`absolute h-4 w-4 text-[var(--color-accent)] transition-all duration-300 ${
          mode === 'system' ? 'opacity-100 rotate-0 scale-100' : 'opacity-0 rotate-90 scale-75'
        }`}
      />
      <Sun
        className={`absolute h-4 w-4 text-[var(--color-warning)] transition-all duration-300 ${
          mode === 'light' ? 'opacity-100 rotate-0 scale-100' : 'opacity-0 -rotate-90 scale-75'
        }`}
      />
      <Moon
        className={`absolute h-4 w-4 text-[var(--color-primary)] transition-all duration-300 ${
          mode === 'dark' ? 'opacity-100 rotate-0 scale-100' : 'opacity-0 rotate-90 scale-75'
        }`}
      />
    </button>
  )
}
