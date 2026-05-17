import { create } from 'zustand'
import type { AuthCheckResponse, LoginResponse } from '@/types'

const TOKEN_KEY = 'soloqueue_token'
const USER_KEY = 'soloqueue_user'

function getStoredToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

function setStoredToken(token: string | null) {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token)
  } else {
    localStorage.removeItem(TOKEN_KEY)
  }
}

function getStoredUser(): string | null {
  return localStorage.getItem(USER_KEY)
}

function setStoredUser(user: string | null) {
  if (user) {
    localStorage.setItem(USER_KEY, user)
  } else {
    localStorage.removeItem(USER_KEY)
  }
}

function getApiBase(): string {
  return '/api/auth'
}

interface AuthState {
  token: string | null
  user: string | null
  isAuthenticated: boolean
  isLoading: boolean
  error: string | null
  login: (user: string, password: string) => Promise<void>
  logout: () => Promise<void>
  checkAuth: () => Promise<boolean>
  clearError: () => void
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: getStoredToken(),
  user: getStoredUser(),
  isAuthenticated: !!getStoredToken(),
  isLoading: true,
  error: null,

  login: async (user: string, password: string) => {
    set({ isLoading: true, error: null })
    try {
      const res = await fetch(`${getApiBase()}/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user, password }),
      })
      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: 'Login failed' }))
        throw new Error(err.error || `HTTP ${res.status}`)
      }
      const data: LoginResponse = await res.json()
      setStoredToken(data.token)
      setStoredUser(data.user)
      set({
        token: data.token,
        user: data.user,
        isAuthenticated: true,
        isLoading: false,
        error: null,
      })
    } catch (err) {
      setStoredToken(null)
      setStoredUser(null)
      set({
        token: null,
        user: null,
        isAuthenticated: false,
        isLoading: false,
        error: err instanceof Error ? err.message : 'Login failed',
      })
      throw err
    }
  },

  logout: async () => {
    const { token } = get()
    if (token) {
      try {
        await fetch(`${getApiBase()}/logout`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${token}`,
          },
        })
      } catch {
        // ignore network errors on logout
      }
    }
    setStoredToken(null)
    setStoredUser(null)
    set({
      token: null,
      user: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
    })
  },

  checkAuth: async () => {
    const token = getStoredToken()
    if (!token) {
      set({ isAuthenticated: false, isLoading: false, token: null, user: null })
      return false
    }
    try {
      const res = await fetch(`${getApiBase()}/check`, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      })
      const data: AuthCheckResponse = await res.json()
      if (data.authenticated) {
        setStoredUser(data.user ?? null)
        set({
          token,
          user: data.user ?? null,
          isAuthenticated: true,
          isLoading: false,
          error: null,
        })
        return true
      }
      setStoredToken(null)
      setStoredUser(null)
      set({ token: null, user: null, isAuthenticated: false, isLoading: false })
      return false
    } catch {
      setStoredToken(null)
      setStoredUser(null)
      set({ token: null, user: null, isAuthenticated: false, isLoading: false })
      return false
    }
  },

  clearError: () => set({ error: null }),
}))
