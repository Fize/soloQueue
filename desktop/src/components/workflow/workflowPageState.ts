import type { ConnectionMode } from '@/stores/connectionStore'

/**
 * Local Electron requests must wait for the main-process health check. Browser
 * development and remote mode have no local child-process readiness gate.
 */
export function shouldWaitForWorkflowBackend(
  isElectron: boolean,
  mode: ConnectionMode,
  backendRunning: boolean,
): boolean {
  return isElectron && mode === 'local' && !backendRunning
}
