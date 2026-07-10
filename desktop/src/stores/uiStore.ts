import { create } from 'zustand'
import { ThemeMode, getStoredTheme, setTheme as applyThemeInLib } from '@/lib/theme'

export type LanguageMode = 'en' | 'zh'

const LANGUAGE_KEY = 'soloqueue_language'

interface UIState {
  theme: ThemeMode
  language: LanguageMode
  setTheme: (theme: ThemeMode) => void
  setLanguage: (lang: LanguageMode) => void
}

export const useUIStore = create<UIState>((set) => ({
  theme: getStoredTheme(),
  language: (localStorage.getItem(LANGUAGE_KEY) as LanguageMode) ?? 'en',
  setTheme: (theme) => {
    applyThemeInLib(theme)
    set({ theme })
  },
  setLanguage: (language) => {
    localStorage.setItem(LANGUAGE_KEY, language)
    set({ language })
  },
}))
