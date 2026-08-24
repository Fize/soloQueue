import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import App from '@/App'
import '@/index.css'

// Theme initialization — runs before React render to prevent flash
import { getStoredTheme, applyTheme, listenSystemTheme } from '@/lib/theme'
import { useConnectionStore } from '@/stores/connectionStore'
import { captureBeforeInstallPrompt, registerServiceWorker } from '@/lib/pwa'

const initial = getStoredTheme()
applyTheme(initial)

// Keep listening even after initial load (handles system-toggle while app is open)
listenSystemTheme()

// Capture the one-shot browser event before async config loading can delay React mounting.
captureBeforeInstallPrompt()

if (import.meta.env.PROD) {
  void registerServiceWorker()
}

async function bootstrap() {
  await useConnectionStore.getState().loadConfig()
  ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
    <StrictMode>
      <App />
    </StrictMode>
  )
}

void bootstrap()
