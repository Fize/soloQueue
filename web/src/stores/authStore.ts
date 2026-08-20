import { create } from 'zustand'

const USER_KEY = 'soloqueue_user'

function getStoredUser(): string | null {
  try {
    return window.localStorage?.getItem(USER_KEY) ?? null
  } catch {
    return null
  }
}

function setStoredUser(user: string | null) {
  try {
    if (user) {
      window.localStorage?.setItem(USER_KEY, user)
    } else {
      window.localStorage?.removeItem(USER_KEY)
    }
  } catch { /* ignore */ }
}

interface AuthState {
  user: string | null
  setUser: (user: string | null) => void
}

export const useAuthStore = create<AuthState>((set) => ({
  user: getStoredUser(),

  setUser: (user) => {
    setStoredUser(user)
    set({ user })
  },
}))
