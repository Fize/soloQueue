import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import App from '@/App'
import '@/index.css'

// Intercept fetch calls in Electron file:// environment to target the Go backend port
if (window.location.protocol === 'file:') {
  const originalFetch = window.fetch
  window.fetch = function (input, init) {
    const port = (window as any).electronAPI?.backendPort || 57647
    if (typeof input === 'string' && input.startsWith('/api')) {
      input = `http://127.0.0.1:${port}${input}`
    } else if (input instanceof Request && input.url.startsWith('/api')) {
      const newUrl = `http://127.0.0.1:${port}${input.url}`
      input = new Request(newUrl, input)
    }
    return originalFetch(input, init)
  }
}

// Theme initialization — runs before React render to prevent flash
import { getStoredTheme, applyTheme, listenSystemTheme } from '@/lib/theme'

const initial = getStoredTheme()
applyTheme(initial)

// Keep listening even after initial load (handles system-toggle while app is open)
listenSystemTheme()

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <StrictMode>
    <App />
  </StrictMode>
)
