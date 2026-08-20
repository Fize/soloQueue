import { useCallback } from 'react'
import { useConnectionStore } from '@/stores/connectionStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { wsManager } from '@/lib/websocket'
import {
  Wifi,
  WifiOff,
  Server,
  ExternalLink,
  Save,
  Loader2,
} from 'lucide-react'

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
      // The active socket may target the previous connection mode/origin.
      wsManager.disconnect()
      await wsManager.connect()
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

    </div>
  )
}

export default ConnectionTab
