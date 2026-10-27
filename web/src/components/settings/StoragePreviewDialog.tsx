import { useEffect, useState } from 'react'
import { useTranslation } from '@/lib/i18n'
import type { StoragePreview } from '@/lib/api/storage-api'
import { formatStorageBytes } from '@/lib/api/storage-api'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

export function StoragePreviewDialog({ preview, mode, busy, error, onClose, onConfirm }: {
  preview: StoragePreview | null; mode: 'save' | 'cleanup'; busy: boolean; error?: string
  onClose: () => void; onConfirm: () => void
}) {
  const { t } = useTranslation()
  const [expiredPreviewId, setExpiredPreviewId] = useState('')
  const expired = !!preview && expiredPreviewId === preview.preview_id
  useEffect(() => {
    if (!preview) return
    const timeout = setTimeout(() => setExpiredPreviewId(preview.preview_id), Math.min(2147483647, Math.max(0, Date.parse(preview.expires_at) - Date.now())))
    return () => clearTimeout(timeout)
  }, [preview])
  return <Dialog open={!!preview} onOpenChange={(open) => { if (!open && !busy) onClose() }}>
    <DialogContent className="max-w-xl">
      <DialogHeader><DialogTitle>{t('storage.previewTitle')}</DialogTitle><DialogDescription>{t('storage.previewDescription')}</DialogDescription></DialogHeader>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {preview && <div className="space-y-3 text-sm">
        <p>{t('storage.willRemove')}: <strong>{preview.files} {t('storage.files')} · {formatStorageBytes(preview.bytes)}</strong></p>
        {preview.files === 0 && <p className="text-muted-foreground">{t('storage.noDelete')}</p>}
        <ul className="space-y-1">{preview.categories.map((category) => <li key={category.id} className="flex justify-between gap-3">
          <span>{t(`storage.${category.id}`)}</span><span>{category.files} · {formatStorageBytes(category.bytes)}</span>
        </li>)}</ul>
        {preview.categories.some((category) => category.id === 'session_history' && category.files > 0) && <p className="text-destructive">{t('storage.historyWarning')}</p>}
        <p className="text-xs text-muted-foreground">{t('storage.protected')}: {preview.protected_files}</p>
        <p className="text-xs text-muted-foreground">{t('storage.expires')}: {new Date(preview.expires_at).toLocaleString()}</p>
        {!!preview.errors?.length && <div role="alert" className="text-xs text-destructive">{t('storage.partial')}<ul>{preview.errors.map((error, index) => <li key={index}>{error}</li>)}</ul></div>}
        {expired && <p role="alert" className="text-destructive">{t('storage.stale')}</p>}
      </div>}
      <DialogFooter>
        <Button variant="outline" size="sm" disabled={busy} onClick={onClose}>{t('storage.cancel')}</Button>
        <Button variant="destructive" size="sm" disabled={busy || expired || !preview || (mode === 'cleanup' && preview.files === 0)} onClick={onConfirm}>
          {busy ? t('storage.busy') : t(mode === 'save' ? 'storage.confirmSave' : 'storage.confirmCleanup')}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
