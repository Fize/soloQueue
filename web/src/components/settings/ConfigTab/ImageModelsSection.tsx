import { useEffect, useState } from 'react'
import { getToolsConfig, patchToolsConfig } from '@/lib/api'
import { useTranslation } from '@/lib/i18n'
import type { ImageModelConfig } from '@/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Select } from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'

const emptyModel: ImageModelConfig = {
  id: '',
  name: '',
  provider: 'tencent',
  secretId: '',
  secretIdEnv: '',
  secretKey: '',
  secretKeyEnv: '',
  apiKey: '',
  apiKeyEnv: '',
  apiBaseHost: 'aiart.tencentcloudapi.com',
  region: 'ap-guangzhou',
  enabled: false,
  isDefault: false,
}

export function validateImageModels(models: ImageModelConfig[]): string | null {
  const ids = new Set<string>()
  for (const m of models) {
    if (!m.id.trim() || !m.name.trim()) return 'config.imageRequired'
    if (ids.has(m.id)) return 'config.imageDuplicate'
    ids.add(m.id)
    if (m.provider !== 'tencent') return 'config.imageProviderError'
    if (m.apiBaseHost && !/^[a-zA-Z0-9.-]+(?::[0-9]+)?$/.test(m.apiBaseHost))
      return 'config.imageHostError'
    if (m.enabled && (!m.apiBaseHost.trim() || !m.region.trim()))
      return 'config.imageEndpointRequired'
    if (
      m.enabled &&
      (!(m.secretId.trim() || m.secretIdEnv.trim()) ||
        !(m.secretKey.trim() || m.secretKeyEnv.trim()))
    )
      return 'config.imageCredentialsError'
    if (m.isDefault && !m.enabled) return 'config.imageDefaultError'
  }
  if (models.some((m) => m.enabled) && models.filter((m) => m.enabled && m.isDefault).length !== 1)
    return 'config.imageDefaultError'
  return null
}

export function ImageModelsSection() {
  const { t } = useTranslation()
  const [models, setModels] = useState<ImageModelConfig[]>([])
  const [loadAttempt, setLoadAttempt] = useState(0)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState<ImageModelConfig | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [transition, setTransition] = useState<{
    models: ImageModelConfig[]
    replace: boolean
  } | null>(null)
  const [replacement, setReplacement] = useState('')
  useEffect(() => {
    let active = true
    getToolsConfig()
      .then((c) => {
        if (active) {
          setModels((c.imageModels || []).map((m) => ({ ...emptyModel, ...m })))
          setLoaded(true)
        }
      })
      .catch((e) => {
        if (active) setError(e.message)
      })
    return () => {
      active = false
    }
  }, [loadAttempt])
  async function persist(next: ImageModelConfig[]) {
    const issue = validateImageModels(next)
    if (issue) {
      setError(t(issue))
      return false
    }
    setSaving(true)
    setError('')
    try {
      const result = await patchToolsConfig({ imageModels: next })
      setModels(result.imageModels || [])
      return true
    } catch (e) {
      setError((e as Error).message)
      return false
    } finally {
      setSaving(false)
    }
  }
  async function saveForm() {
    if (!form) return
    if (models.some((m) => m.id === form.id && m.id !== editingId)) {
      setError(t('config.imageDuplicate'))
      return
    }
    let next =
      editingId === null ? [...models, form] : models.map((m) => (m.id === editingId ? form : m))
    if (form.enabled && form.isDefault)
      next = next.map((m) => ({ ...m, isDefault: m.id === form.id }))
    if (
      next.some((m) => m.enabled) &&
      !next.some((m) => m.enabled && m.isDefault) &&
      editingId !== null &&
      models.some((m) => m.id === editingId && m.isDefault)
    ) {
      setReplacement('')
      setTransition({ models: next, replace: true })
      return
    }
    if (await persist(next)) setForm(null)
  }
  function removeOrDisable(model: ImageModelConfig, remove: boolean) {
    const next = remove
      ? models.filter((m) => m.id !== model.id)
      : models.map((m) => (m.id === model.id ? { ...m, enabled: false, isDefault: false } : m))
    const replace = next.some((m) => m.enabled) && !next.some((m) => m.enabled && m.isDefault)
    if (remove || replace) {
      setError('')
      setReplacement('')
      setTransition({ models: next, replace })
    } else void persist(next)
  }
  async function confirmTransition() {
    if (!transition) return
    const next = transition.replace
      ? transition.models.map((m) =>
          replacement === 'off'
            ? { ...m, enabled: false, isDefault: false }
            : { ...m, isDefault: `model:${m.id}` === replacement }
        )
      : transition.models
    if (await persist(next)) {
      setTransition(null)
      setForm(null)
    }
  }
  return (
    <section className="space-y-4 min-w-0">
      <div className="flex flex-wrap gap-3 items-center justify-between">
        <h3 className="font-semibold">{t('config.imageTitle')}</h3>
        <Button
          disabled={!loaded || saving}
          onClick={() => {
            setEditingId(null)
            setForm({ ...emptyModel })
            setError('')
          }}
        >
          {t('common.add')}
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">{t('config.imageHint')}</p>
      {error && !form && !transition && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {!loaded && error && (
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
      {!loaded && !error && <p>{t('common.loading')}</p>}
      {loaded && models.length === 0 && (
        <p className="rounded-lg border border-dashed p-8 text-center text-muted-foreground">
          {t('common.none')}
        </p>
      )}
      {models.map((m) => (
        <div
          key={m.id}
          className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3"
        >
          <div className="min-w-0 break-words">
            <p className="font-medium">
              {m.name}{' '}
              {m.isDefault && (
                <span className="text-xs text-primary">★ {t('config.llmDefault')}</span>
              )}
            </p>
            <p className="text-xs text-muted-foreground">{m.id} · Tencent</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center">
              <span className="sr-only">{`${t('common.enabled')} ${m.name}`}</span>
              <Switch
                disabled={saving}
                checked={m.enabled}
                onCheckedChange={(enabled) =>
                  enabled
                    ? void persist(
                        models.map((x) =>
                          x.id === m.id
                            ? { ...x, enabled: true, isDefault: !models.some((y) => y.enabled) }
                            : x
                        )
                      )
                    : removeOrDisable(m, false)
                }
              />
            </label>
            {m.enabled && !m.isDefault && (
              <Button
                size="xs"
                variant="outline"
                disabled={saving}
                onClick={() =>
                  void persist(models.map((x) => ({ ...x, isDefault: x.id === m.id })))
                }
              >
                {t('config.llmSetAsDefault')}
              </Button>
            )}
            <Button
              size="xs"
              variant="ghost"
              disabled={saving}
              onClick={() => {
                setEditingId(m.id)
                setForm({ ...emptyModel, ...m })
                setError('')
              }}
            >
              {t('common.edit')}
            </Button>
            <Button
              size="xs"
              variant="ghost"
              disabled={saving}
              onClick={() => removeOrDisable(m, true)}
            >
              {t('common.delete')}
            </Button>
          </div>
        </div>
      ))}
      <Dialog
        open={!!form && !transition}
        onOpenChange={(open) => {
          if (!open && !saving) setForm(null)
        }}
      >
        <DialogContent className="max-w-xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t('config.imageTitle')}</DialogTitle>
            <DialogDescription>{t('config.imageCredentialsHint')}</DialogDescription>
          </DialogHeader>
          {form && (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                {(
                  [
                    'id',
                    'name',
                    'secretId',
                    'secretIdEnv',
                    'secretKey',
                    'secretKeyEnv',
                    'apiBaseHost',
                    'region',
                  ] as const
                ).map((key) => (
                  <label key={key} className="text-sm space-y-1">
                    {t(`config.imageField_${key}`)}
                    <Input
                      disabled={saving}
                      type={key === 'secretKey' ? 'password' : 'text'}
                      value={form[key]}
                      onChange={(e) => setForm({ ...form, [key]: e.target.value })}
                    />
                  </label>
                ))}
                <label className="text-sm space-y-1">
                  {t('config.llmProviderLabel')}
                  <Input value="Tencent (tencent)" disabled />
                </label>
                <div className="flex flex-wrap gap-4 items-center">
                  <label className="flex items-center gap-2 text-sm">
                    <Switch
                      disabled={saving}
                      checked={form.enabled}
                      onCheckedChange={(enabled) =>
                        setForm({
                          ...form,
                          enabled,
                          isDefault: enabled
                            ? form.isDefault || !models.some((m) => m.enabled && m.id !== editingId)
                            : false,
                        })
                      }
                    />
                    {t('common.enabled')}
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Switch
                      checked={form.isDefault}
                      disabled={saving || !form.enabled}
                      onCheckedChange={(isDefault) => setForm({ ...form, isDefault })}
                    />
                    {t('config.llmDefault')}
                  </label>
                </div>
              </div>
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
              <p className="text-xs text-muted-foreground">{t('config.imageDefaultHint')}</p>
              <DialogFooter>
                <Button variant="outline" disabled={saving} onClick={() => setForm(null)}>
                  {t('common.cancel')}
                </Button>
                <Button disabled={saving} onClick={() => void saveForm()}>
                  {t(saving ? 'common.saving' : 'common.save')}
                </Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>
      <Dialog
        open={!!transition}
        onOpenChange={(open) => {
          if (!open && !saving) setTransition(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('common.confirm')}</DialogTitle>
            <DialogDescription>
              {t(transition?.replace ? 'config.imageReplacementHint' : 'config.imageDeleteHint')}
            </DialogDescription>
          </DialogHeader>
          {transition?.replace && (
            <Select
              label={t('config.llmDefault')}
              disabled={saving}
              value={replacement}
              onChange={setReplacement}
              options={[
                { value: '', label: t('config.llmSelectModel') },
                ...transition.models
                  .filter((m) => m.enabled)
                  .map((m) => ({ value: `model:${m.id}`, label: m.name })),
                { value: 'off', label: t('config.imageDisableAll') },
              ]}
            />
          )}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button disabled={saving} variant="outline" onClick={() => setTransition(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              disabled={saving || (!!transition?.replace && !replacement)}
              onClick={() => void confirmTransition()}
            >
              {t('common.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
