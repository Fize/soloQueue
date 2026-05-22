import { create } from 'zustand'
import type { AuthCheckResponse } from '@/types'

const USER_KEY = 'soloqueue_user'

function getStoredUser(): string | null {
  if (
    typeof window === 'undefined' ||
    typeof window.localStorage === 'undefined' ||
    typeof window.localStorage.getItem !== 'function'
  ) {
    return null
  }
  return window.localStorage.getItem(USER_KEY)
}

function setStoredUser(user: string | null) {
  if (
    typeof window === 'undefined' ||
    typeof window.localStorage === 'undefined' ||
    typeof window.localStorage.setItem !== 'function' ||
    typeof window.localStorage.removeItem !== 'function'
  ) {
    return
  }
  if (user) {
    window.localStorage.setItem(USER_KEY, user)
  } else {
    window.localStorage.removeItem(USER_KEY)
  }
}

function getApiBase(): string {
  return '/api/auth'
}

interface AuthState {
  user: string | null
  isAuthenticated: boolean
  isLoading: boolean
  error: string | null
  login: (user: string, password: string) => Promise<void>
  logout: () => Promise<void>
  checkAuth: () => Promise<boolean>
  clearError: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  user: getStoredUser(),
  isAuthenticated: !!getStoredUser(),
  isLoading: true,
  error: null,

  login: async () => {
    // No-op under Basic Auth (handled by browser native prompt)
  },

  logout: async () => {
    setStoredUser(null)
    set({
      user: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
    })
    try {
      // Send invalid credentials to overwrite browser basic auth cache
      await fetch(`${getApiBase()}/check`, {
        headers: {
          Authorization: 'Basic ' + btoa('logout:logout'),
        },
      })
    } catch {
      // Expected 401
    }
    window.location.reload()
  },

  checkAuth: async () => {
    try {
      const res = await fetch(`${getApiBase()}/check`)
      if (res.status === 401) {
        setStoredUser(null)
        set({ user: null, isAuthenticated: false, isLoading: false })
        return false
      }
      const data: AuthCheckResponse = await res.json()
      if (data.authenticated) {
        setStoredUser(data.user ?? null)
        set({
          user: data.user ?? null,
          isAuthenticated: true,
          isLoading: false,
          error: null,
        })
        return true
      }
      setStoredUser(null)
      set({ user: null, isAuthenticated: false, isLoading: false })
      return false
    } catch {
      setStoredUser(null)
      set({ user: null, isAuthenticated: false, isLoading: false })
      return false
    }
  },

  clearError: () => set({ error: null }),
}))
