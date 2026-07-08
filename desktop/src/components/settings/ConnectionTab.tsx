import { useEffect, useCallback, useRef } from 'react'
import { useConnectionStore, type BackendStatus } from '@/stores/connectionStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import {
  Wifi,
  WifiOff,
  Play,
  Square,
  RefreshCw,
  Save,
  Loader2,
  Server,
  ExternalLink,
  Terminal,
  Circle,
} from 'lucide-react'

const isElectron = typeof window !== 'undefined' && !!(window as any).electronAPI

// ─── Backend Logs Panel ──────────────────────────────────────────────────────

function BackendLogs() {
  const logs = useConnectionStore((s) => s.backendLogs)
  const clearLogs = useConnectionStore((s) => s.clearBackendLogs)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [logs])

  if (logs.length === 0) return null

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-semibold text-foreground/70 flex items-center gap-1.5">
          <Terminal className="h-3 w-3" />
          Backend Logs
        </h3>
        <button
          onClick={clearLogs}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          Clear
        </button>
      </div>
      <div
        ref={scrollRef}
        className="bg-card/80 border border-border/40 rounded-md max-h-40 overflow-y-auto p-2"
      >
        {logs.map((line, i) => (
          <div
            key={i}
            className="text-xs font-mono text-muted-foreground leading-relaxed whitespace-pre-wrap break-all"
          >
            {line}
          </div>
        ))}
      </div>
    </div>
  )
}

// ─── Backend Management Section ──────────────────────────────────────────────

function BackendManagement() {
  const backendStatus = useConnectionStore((s) => s.backendStatus)
  const setBackendStatus = useConnectionStore((s) => s.setBackendStatus)
  const appendBackendLog = useConnectionStore((s) => s.appendBackendLog)

  // Poll backend status and listen for push events
  useEffect(() => {
    if (!isElectron) return

    const ea = (window as any).electronAPI

    // Initial status
    ea.getBackendStatus().then((s: BackendStatus) => setBackendStatus(s))

    // Push events
    const unsubStatus = ea.onBackendStatusChanged((s: BackendStatus) => setBackendStatus(s))
    const unsubLog = ea.onBackendLog((line: string) => appendBackendLog(line))

    // Periodic polling fallback
    const interval = setInterval(async () => {
      try {
        const s = await ea.getBackendStatus()
        setBackendStatus(s)
      } catch { /* ignore */ }
    }, 5000)

    return () => {
      unsubStatus()
      unsubLog()
      clearInterval(interval)
    }
  }, [setBackendStatus, appendBackendLog])

  const handleStart = async () => {
    if (!isElectron) return
    try {
      const ea = (window as any).electronAPI
      const result = await ea.startBackend()
      if (!result.success) {
        toast.error(result.error || 'Failed to start backend')
      } else {
        toast.success('Backend started')
      }
    } catch (err) {
      toast.error('Failed to start backend')
    }
  }

  const handleStop = async () => {
    if (!isElectron) return
    try {
      const ea = (window as any).electronAPI
      await ea.stopBackend()
      toast.success('Backend stopped')
    } catch {
      toast.error('Failed to stop backend')
    }
  }

  const handleRestart = async () => {
    if (!isElectron) return
    try {
      const ea = (window as any).electronAPI
      const result = await ea.restartBackend()
      if (!result.success) {
        toast.error(result.error || 'Failed to restart backend')
      } else {
        toast.success('Backend restarted')
      }
    } catch {
      toast.error('Failed to restart backend')
    }
  }

  const uptime = backendStatus.uptime
    ? formatUptime(backendStatus.uptime)
    : null

  return (
    <div className="space-y-4">
      {/* Status bar */}
      <div className="flex items-center justify-between p-3 bg-card/60 border border-border/40 rounded-lg">
        <div className="flex items-center gap-3">
          <div className="relative">
            <Server className="h-5 w-5 text-muted-foreground" />
            <Circle
              className={`h-2 w-2 absolute -bottom-0.5 -right-0.5 ${
                backendStatus.running
                  ? 'text-green-500 fill-green-500'
                  : 'text-muted-foreground fill-muted-foreground'
              }`}
            />
          </div>
          <div>
            <div className="text-sm font-semibold flex items-center gap-2">
              {backendStatus.running ? (
                <Badge variant="default" className="bg-green-500/15 text-green-600 border-green-500/20 text-[10px] px-1.5 py-0">
                  RUNNING
                </Badge>
              ) : (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0">
                  STOPPED
                </Badge>
              )}
            </div>
            <div className="text-xs text-muted-foreground mt-0.5">
              {backendStatus.running && (
                <>
                  PID: {backendStatus.pid}
                  {uptime && <span className="ml-2">Uptime: {uptime}</span>}
                </>
              )}
              {!backendStatus.running && 'Backend service is not running'}
            </div>
          </div>
        </div>
      </div>

      {/* Control buttons */}
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={handleStart}
          disabled={backendStatus.running}
          className="flex-1"
        >
          <Play className="h-3.5 w-3.5 mr-1.5" />
          Start
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={handleStop}
          disabled={!backendStatus.running}
          className="flex-1"
        >
          <Square className="h-3.5 w-3.5 mr-1.5" />
          Stop
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={handleRestart}
          disabled={!backendStatus.running}
          className="flex-1"
        >
          <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
          Restart
        </Button>
      </div>

      {/* Logs */}
      <BackendLogs />
    </div>
  )
}

// ─── Connection Mode Section ─────────────────────────────────────────────────

function ConnectionModeSection() {
  const mode = useConnectionStore((s) => s.mode)
  const remoteUrl = useConnectionStore((s) => s.remoteUrl)
  const setMode = useConnectionStore((s) => s.setMode)
  const setRemoteUrl = useConnectionStore((s) => s.setRemoteUrl)

  return (
    <div className="space-y-4">
      {/* Mode toggle */}
      <div>
        <label className="text-xs font-semibold text-foreground/70 mb-2 block">
          Connection Mode
        </label>
        <div className="flex rounded-lg border border-border/40 overflow-hidden">
          <button
            onClick={() => setMode('local')}
            className={`flex-1 flex items-center justify-center gap-2 px-4 py-2.5 text-sm font-medium transition-colors cursor-pointer ${
              mode === 'local'
                ? 'bg-primary text-primary-foreground'
                : 'bg-card/40 text-muted-foreground hover:text-foreground hover:bg-card/60'
            }`}
          >
            <Server className="h-4 w-4" />
            Local
          </button>
          <button
            onClick={() => setMode('remote')}
            className={`flex-1 flex items-center justify-center gap-2 px-4 py-2.5 text-sm font-medium transition-colors cursor-pointer ${
              mode === 'remote'
                ? 'bg-primary text-primary-foreground'
                : 'bg-card/40 text-muted-foreground hover:text-foreground hover:bg-card/60'
            }`}
          >
            <ExternalLink className="h-4 w-4" />
            Remote
          </button>
        </div>
        <p className="text-xs text-muted-foreground mt-1.5">
          {mode === 'local'
            ? 'Connect to the backend running locally on this machine.'
            : 'Connect to a remote backend server. The local backend will not start automatically.'}
        </p>
      </div>

      {/* Remote URL input */}
      {mode === 'remote' && (
        <div>
          <label className="text-xs font-semibold text-foreground/70 mb-2 block">
            Remote Backend URL
          </label>
          <Input
            value={remoteUrl}
            onChange={(e) => setRemoteUrl(e.target.value)}
            placeholder="http://remote-server:57647"
            className="font-mono text-sm"
          />
          <p className="text-xs text-muted-foreground mt-1.5">
            Enter the full URL of the remote backend (e.g., http://192.168.1.100:57647).
            All API and WebSocket connections will be routed to this address.
          </p>
        </div>
      )}

      {/* Connection status indicator */}
      <div className="flex items-center gap-2 p-3 bg-card/60 border border-border/40 rounded-lg">
        {mode === 'local' ? (
          <Wifi className="h-4 w-4 text-green-500" />
        ) : remoteUrl ? (
          <Wifi className="h-4 w-4 text-blue-500" />
        ) : (
          <WifiOff className="h-4 w-4 text-yellow-500" />
        )}
        <span className="text-sm">
          {mode === 'local'
            ? 'Local mode — connecting to backend on this machine'
            : remoteUrl
              ? `Remote mode — connecting to ${remoteUrl}`
              : 'Remote mode — no URL configured'}
        </span>
      </div>
    </div>
  )
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatUptime(ms: number): string {
  const seconds = Math.floor(ms / 1000)
  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)
  if (hours > 0) return `${hours}h ${minutes % 60}m`
  if (minutes > 0) return `${minutes}m ${seconds % 60}s`
  return `${seconds}s`
}

// ─── Main Component ──────────────────────────────────────────────────────────

export function ConnectionTab() {
  const mode = useConnectionStore((s) => s.mode)
  const remoteUrl = useConnectionStore((s) => s.remoteUrl)
  const saving = useConnectionStore((s) => s.saving)
  const saveConfig = useConnectionStore((s) => s.saveConfig)
  const loadConfig = useConnectionStore((s) => s.loadConfig)

  // Load config on mount
  useEffect(() => {
    loadConfig()
  }, [loadConfig])

  const handleSave = useCallback(async () => {
    if (mode === 'remote' && !remoteUrl.trim()) {
      toast.error('Please enter a remote backend URL')
      return
    }
    try {
      await saveConfig()
      toast.success('Connection configuration saved. Restart the app to apply changes.')
    } catch {
      toast.error('Failed to save connection configuration')
    }
  }, [mode, remoteUrl, saveConfig])

  return (
    <div className="space-y-8">
      {/* Section: Connection Mode */}
      <section>
        <h2 className="text-sm font-bold text-foreground mb-4 flex items-center gap-2">
          <Wifi className="h-4 w-4 text-primary" />
          Connection Configuration
        </h2>
        <div className="pl-6 border-l-2 border-primary/20 space-y-6">
          <ConnectionModeSection />

          <div className="flex justify-end">
            <Button onClick={handleSave} disabled={saving} size="sm">
              {saving ? (
                <Loader2 className="h-4 w-4 mr-1.5 animate-spin" />
              ) : (
                <Save className="h-4 w-4 mr-1.5" />
              )}
              Save Configuration
            </Button>
          </div>
        </div>
      </section>

      {/* Section: Backend Management (Electron only) */}
      {isElectron && (
        <section>
          <h2 className="text-sm font-bold text-foreground mb-4 flex items-center gap-2">
            <Server className="h-4 w-4 text-primary" />
            Local Backend Management
          </h2>
          <div className="pl-6 border-l-2 border-primary/20">
            <BackendManagement />
          </div>
        </section>
      )}

      {/* Non-Electron notice */}
      {!isElectron && (
        <section>
          <h2 className="text-sm font-bold text-foreground mb-4 flex items-center gap-2">
            <Server className="h-4 w-4 text-muted-foreground" />
            Local Backend Management
          </h2>
          <div className="pl-6 border-l-2 border-border/20">
            <p className="text-sm text-muted-foreground">
              Backend management is only available in the Electron desktop application.
            </p>
          </div>
        </section>
      )}
    </div>
  )
}

export default ConnectionTab
