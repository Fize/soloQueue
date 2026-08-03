import { useEffect, useCallback } from 'react'
import { useConnectionStore, type BackendStatus } from '@/stores/connectionStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
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
  Circle,
} from 'lucide-react'

const isElectron = typeof window !== 'undefined' && !!(window as any).electronAPI

// ─── Backend Management Section ──────────────────────────────────────────────

function BackendManagement() {
  const backendStatus = useConnectionStore((s) => s.backendStatus)
  const setBackendStatus = useConnectionStore((s) => s.setBackendStatus)
  const { t } = useTranslation()

  // Poll backend status and listen for push events
  useEffect(() => {
    if (!isElectron) return

    const ea = (window as any).electronAPI

    // Initial status
    ea.getBackendStatus().then((s: BackendStatus) => setBackendStatus(s))

    // Push events
    const unsubStatus = ea.onBackendStatusChanged((s: BackendStatus) => setBackendStatus(s))

    // Periodic polling fallback
    const interval = setInterval(async () => {
      try {
        const s = await ea.getBackendStatus()
        setBackendStatus(s)
      } catch { /* ignore */ }
    }, 5000)

    return () => {
      unsubStatus()
      clearInterval(interval)
    }
  }, [setBackendStatus])

  const handleStart = async () => {
    if (!isElectron) return
    try {
      const ea = (window as any).electronAPI
      const result = await ea.startBackend()
      if (!result.success) {
        toast.error(result.error || t('connection.startFailed'))
      } else {
        toast.success(t('connection.started'))
      }
    } catch {
      toast.error(t('connection.startFailed'))
    }
  }

  const handleStop = async () => {
    if (!isElectron) return
    try {
      const ea = (window as any).electronAPI
      await ea.stopBackend()
      toast.success(t('connection.stoppedToast'))
    } catch {
      toast.error(t('connection.stopFailed'))
    }
  }

  const handleRestart = async () => {
    if (!isElectron) return
    try {
      const ea = (window as any).electronAPI
      const result = await ea.restartBackend()
      if (!result.success) {
        toast.error(result.error || t('connection.restartFailed'))
      } else {
        toast.success(t('connection.restarted'))
      }
    } catch {
      toast.error(t('connection.restartFailed'))
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
                  {t('connection.running')}
                </Badge>
              ) : (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0">
                  {t('connection.stopped')}
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
              {!backendStatus.running && t('connection.notRunning')}
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
          {t('connection.start')}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={handleStop}
          disabled={!backendStatus.running}
          className="flex-1"
        >
          <Square className="h-3.5 w-3.5 mr-1.5" />
          {t('connection.stop')}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={handleRestart}
          disabled={!backendStatus.running}
          className="flex-1"
        >
          <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
          {t('connection.restart')}
        </Button>
      </div>

    </div>
  )
}

// ─── Connection Mode Section ─────────────────────────────────────────────────

function ConnectionModeSection() {
  const mode = useConnectionStore((s) => s.mode)
  const remoteUrl = useConnectionStore((s) => s.remoteUrl)
  const username = useConnectionStore((s) => s.username)
  const password = useConnectionStore((s) => s.password)
  const setMode = useConnectionStore((s) => s.setMode)
  const setRemoteUrl = useConnectionStore((s) => s.setRemoteUrl)
  const setUsername = useConnectionStore((s) => s.setUsername)
  const setPassword = useConnectionStore((s) => s.setPassword)
  const { t } = useTranslation()

  return (
    <div className="space-y-4">
      {/* Mode toggle */}
      <div>
        <label className="text-xs font-semibold text-foreground/70 mb-2 block">
          {t('connection.mode')}
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
            {t('connection.localBtn')}
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
            {t('connection.remoteBtn')}
          </button>
        </div>
        <p className="text-xs text-muted-foreground mt-1.5">
          {mode === 'local'
            ? t('connection.localDesc')
            : t('connection.remoteDesc')}
        </p>
      </div>

      {/* Remote URL and Credentials input */}
      {mode === 'remote' && (
        <div className="space-y-4">
          <div>
            <label className="text-xs font-semibold text-foreground/70 mb-2 block">
              {t('connection.remoteUrl')}
            </label>
            <Input
              value={remoteUrl}
              onChange={(e) => setRemoteUrl(e.target.value)}
              placeholder="http://remote-server:57647"
              className="font-mono text-sm"
            />
            <p className="text-xs text-muted-foreground mt-1.5">
              {t('connection.remoteUrlDesc')}
            </p>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="text-xs font-semibold text-foreground/70 mb-2 block">
                {t('connection.usernameOpt')}
              </label>
              <Input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="admin"
                className="font-mono text-sm"
              />
            </div>
            <div>
              <label className="text-xs font-semibold text-foreground/70 mb-2 block">
                {t('connection.passwordOpt')}
              </label>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className="font-mono text-sm"
              />
            </div>
          </div>
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
            ? t('connection.localStatus')
            : remoteUrl
              ? t('connection.remoteStatus', { url: remoteUrl })
              : t('connection.remoteStatusNoUrl')}
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
  const { t } = useTranslation()

  const handleSave = useCallback(async () => {
    if (mode === 'remote' && !remoteUrl.trim()) {
      toast.error(t('connection.enterUrlError'))
      return
    }
    try {
      await saveConfig()
      // When switching to remote mode, stop the local Go backend
      if (mode === 'remote' && isElectron) {
        try {
          const ea = (window as any).electronAPI
          await ea.stopBackend()
        } catch { /* backend may not be running, ignore */ }
      }
      toast.success(t('connection.saveSuccess'))
    } catch {
      toast.error(t('connection.saveFail'))
    }
  }, [mode, remoteUrl, saveConfig, t])

  return (
    <div className="space-y-8">
      {/* Section: Connection Mode */}
      <section>
        <h2 className="text-sm font-bold text-foreground mb-4 flex items-center gap-2">
          <Wifi className="h-4 w-4 text-primary" />
          {t('connection.title')}
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
              {t('connection.save')}
            </Button>
          </div>
        </div>
      </section>

      {/* Section: Backend Management (Electron only) */}
      {isElectron && (
        <section>
          <h2 className="text-sm font-bold text-foreground mb-4 flex items-center gap-2">
            <Server className="h-4 w-4 text-primary" />
            {t('connection.localBackendManagement')}
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
            {t('connection.localBackendManagement')}
          </h2>
          <div className="pl-6 border-l-2 border-border/20">
            <p className="text-sm text-muted-foreground">
              {t('connection.electronOnly')}
            </p>
          </div>
        </section>
      )}
    </div>
  )
}

export default ConnectionTab
