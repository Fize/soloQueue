import { useEffect, useState } from 'react'
import { getToolsConfig, patchToolsConfig } from '@/lib/api'
import { useTranslation } from '@/lib/i18n'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

export function SearchToolsSection() {
  const { t } = useTranslation()
  const [form, setForm] = useState<{
    tavilyApiKey: string
    tavilyApiKeyEnv: string
    webSearchTimeoutMs: number
  } | null>(null)
  const [loadAttempt, setLoadAttempt] = useState(0)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  useEffect(() => {
    let active = true
    getToolsConfig()
      .then((c) => {
        if (active)
          setForm({
            tavilyApiKey: c.tavilyApiKey || '',
            tavilyApiKeyEnv: c.tavilyApiKeyEnv || '',
            webSearchTimeoutMs: c.webSearchTimeoutMs,
          })
      })
      .catch((e) => {
        if (active) setError(e.message)
      })
    return () => {
      active = false
    }
  }, [loadAttempt])
  async function save() {
    if (!form) return
    setSaving(true)
    setError('')
    setSaved(false)
    try {
      await patchToolsConfig(form)
      setSaved(true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }
  return (
    <section className="rounded-xl border bg-card p-5 space-y-4">
      <h3 className="font-semibold">{t('config.searchTitle')}</h3>
      <p className="text-sm text-muted-foreground">{t('config.searchHint')}</p>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      {!form && error && (
        <Button
          variant="outline"
          onClick={() => {
            setError('')
            setLoadAttempt((n) => n + 1)
          }}
        >
          {t('config.retryLoad')}
        </Button>
      )}
      {!form && !error && <p>{t('common.loading')}</p>}
      {form && (
        <>
          <div className="grid gap-4 sm:grid-cols-2">
            <label className="space-y-2 text-sm">
              {t('config.toolsTavilyApiKey')}
              <Input
                disabled={saving}
                type="password"
                value={form.tavilyApiKey}
                onChange={(e) => {
                  setSaved(false)
                  setForm({ ...form, tavilyApiKey: e.target.value })
                }}
              />
            </label>
            <label className="space-y-2 text-sm">
              {t('config.toolsTavilyApiKeyEnv')}
              <Input
                disabled={saving}
                value={form.tavilyApiKeyEnv}
                onChange={(e) => {
                  setSaved(false)
                  setForm({ ...form, tavilyApiKeyEnv: e.target.value })
                }}
              />
            </label>
            <label className="space-y-2 text-sm">
              {t('config.toolsWebSearchTimeout')}
              <Input
                disabled={saving}
                type="number"
                min={0}
                value={form.webSearchTimeoutMs}
                onChange={(e) => {
                  setSaved(false)
                  setForm({ ...form, webSearchTimeoutMs: Number(e.target.value) })
                }}
              />
            </label>
          </div>
          <div className="flex items-center gap-3">
            <Button disabled={saving} onClick={() => void save()}>
              {t(saving ? 'common.saving' : 'common.save')}
            </Button>
            {saved && (
              <p role="status" className="text-sm">
                {t('common.saved')}
              </p>
            )}
          </div>
        </>
      )}
    </section>
  )
}
