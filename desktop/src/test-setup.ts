import '@testing-library/jest-dom/vitest'

// Mock localStorage globally for testing environment
if (typeof window !== 'undefined') {
  const localStorageMock = (() => {
    let store: Record<string, string> = {}
    return {
      getItem: (key: string) => store[key] || null,
      setItem: (key: string, value: string) => {
        store[key] = value.toString()
      },
      removeItem: (key: string) => {
        delete store[key]
      },
      clear: () => {
        store = {}
      },
      length: 0,
      key: (_index: number) => null,
    }
  })()

  Object.defineProperty(window, 'localStorage', {
    value: localStorageMock,
    writable: true,
  })

  // Also define it on global object for direct access
  Object.defineProperty(global, 'localStorage', {
    value: localStorageMock,
    writable: true,
  })
}
