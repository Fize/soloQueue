import { Button } from '@/components/ui/button'
import { usePWAInstall } from '@/hooks/usePWAInstall'

function InstallGuide({ onClose }: { onClose: () => void }) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="pwa-install-guide-title"
      className="fixed inset-x-4 bottom-4 z-[120] mx-auto max-w-lg rounded-xl border border-border bg-card p-4 text-card-foreground shadow-2xl sm:inset-x-auto sm:right-6 sm:w-[min(28rem,calc(100vw-3rem))]"
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 id="pwa-install-guide-title" className="text-sm font-semibold">
            Install SoloQueue
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Add SoloQueue to your home screen or app launcher for a focused workspace.
          </p>
        </div>
        <Button type="button" variant="ghost" size="icon-xs" onClick={onClose} aria-label="Close install guide">
          ×
        </Button>
      </div>
      <ol className="mt-3 list-decimal space-y-2 pl-5 text-xs text-muted-foreground">
        <li>
          <strong className="text-foreground">Desktop browser:</strong> open the browser menu and choose
          Install or Add to Dock/Home screen.
        </li>
        <li>
          <strong className="text-foreground">Mobile browser:</strong> open Share or the browser menu and
          choose Add to Home Screen.
        </li>
      </ol>
      <div className="mt-4 flex justify-end">
        <Button type="button" variant="outline" size="sm" onClick={onClose}>
          Done
        </Button>
      </div>
    </div>
  )
}

export function PWAInstallPrompt() {
  const { status, guideOpen, install, dismiss, showGuide, closeGuide } = usePWAInstall()

  if (status === 'checking' || status === 'installed' || status === 'dismissed' || status === 'unsupported') {
    return null
  }

  const nativePrompt = status === 'available'
  return (
    <>
      <section
        role="region"
        aria-label="Install SoloQueue"
        className="fixed inset-x-4 bottom-4 z-[110] mx-auto flex max-w-xl items-center justify-between gap-3 rounded-xl border border-border bg-card/95 p-3 text-card-foreground shadow-xl backdrop-blur sm:inset-x-auto sm:right-6 sm:w-[min(28rem,calc(100vw-3rem))]"
      >
        <p className="text-xs leading-5 text-muted-foreground">
          Install <span className="font-semibold text-foreground">SoloQueue</span> for quick access.
        </p>
        <div className="flex shrink-0 items-center gap-2">
          <Button type="button" size="sm" onClick={() => (nativePrompt ? void install() : showGuide())}>
            {nativePrompt ? 'Install' : 'Install Guide'}
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={dismiss}>
            Not now
          </Button>
        </div>
      </section>
      {guideOpen && <InstallGuide onClose={closeGuide} />}
    </>
  )
}
