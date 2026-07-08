import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import App from '@/App'
import '@/index.css'

// Intercept fetch calls in Electron file:// environment to target the Go backend port.
// When remote connection mode is enabled, rewrite to the remote URL instead.
if (window.location.protocol === 'file:') {
  const originalFetch = window.fetch
  window.fetch = function (input, init) {
    // Dynamically read connection mode from localStorage at interception time
    let baseUrl: string
    try {
      const connMode = localStorage.getItem('soloqueue_connection_mode')
      const remoteUrl = localStorage.getItem('soloqueue_remote_url')
      if (connMode === 'remote' && remoteUrl) {
        baseUrl = remoteUrl.replace(/\/+$/, '')
      } else {
        const port = (window as any).electronAPI?.backendPort || 57647
        baseUrl = `http://127.0.0.1:${port}`
      }
    } catch {
      const port = (window as any).electronAPI?.backendPort || 57647
      baseUrl = `http://127.0.0.1:${port}`
    }

    if (typeof input === 'string' && input.startsWith('/api')) {
      input = `${baseUrl}${input}`
    } else if (input instanceof Request && input.url.startsWith('/api')) {
      const newUrl = `${baseUrl}${input.url}`
      input = new Request(newUrl, input)
    } else if (typeof input === 'string' && input === '/healthz') {
      input = `${baseUrl}/healthz`
    } else if (input instanceof Request && input.url === '/healthz') {
      input = new Request(`${baseUrl}/healthz`, input)
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
