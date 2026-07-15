import { useState, useEffect } from 'react'
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  listModels,
  createModel,
  updateModel,
  deleteModel,
  getDefaultModels,
  updateDefaultModels,
} from '@/lib/api'
import type { LLMProvider, LLMModel, DefaultModelsConfig } from '@/types'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { LLMSection } from './ConfigTab/LLMSection'

export function ModelsTab() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [models, setModels] = useState<LLMModel[]>([])
  const [deleteProviderTarget, setDeleteProviderTarget] = useState<LLMProvider | null>(null)
  const [deleteModelTarget, setDeleteModelTarget] = useState<LLMModel | null>(null)
  const [defaultModels, setDefaultModels] = useState<DefaultModelsConfig>({
    expert: '',
    superior: '',
    universal: '',
    fast: '',
    fallback: '',
  })

  const loadData = async () => {
    setLoading(true)
    try {
      const [dbProviders, dbModels, dbDefaults] = await Promise.all([
        listProviders(),
        listModels(),
        getDefaultModels(),
      ])
      setProviders(dbProviders || [])
      setModels(dbModels || [])
      setDefaultModels(
        dbDefaults || { expert: '', superior: '', universal: '', fast: '', fallback: '' }
      )
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const handleSaveDefaults = async () => {
    try {
      await updateDefaultModels(defaultModels)
      toast.success(t('config.toastDefaultsUpdated'))
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleCreateProvider = async (payload: LLMProvider) => {
    await createProvider(payload)
    toast.success(t('config.toastProviderCreated', { name: payload.name }))
    loadData()
  }

  const handleUpdateProviderAction = async (id: string, payload: LLMProvider) => {
    await updateProvider(id, payload)
    toast.success(t('config.toastProviderCreated', { name: payload.name }))
    loadData()
  }

  const handleDeleteProviderAction = (id: string) => {
    const p = providers.find((p) => p.id === id)
    if (p) setDeleteProviderTarget(p)
  }

  const confirmDeleteProvider = async () => {
    if (!deleteProviderTarget) return
    try {
      await deleteProvider(deleteProviderTarget.id)
      setDeleteProviderTarget(null)
      loadData()
      toast.success(t('config.toastProviderDeleted'))
    } catch (err) {
      toast.error((err as Error).message)
      setDeleteProviderTarget(null)
    }
  }

  const handleToggleProviderStatus = async (p: LLMProvider) => {
    try {
      await updateProvider(p.id, { ...p, enabled: !p.enabled })
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSetProviderAsDefault = async (p: LLMProvider) => {
    try {
      await updateProvider(p.id, { ...p, isDefault: true })
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleCreateModel = async (payload: LLMModel) => {
    await createModel(payload)
    toast.success(t('config.toastModelCreated', { name: payload.name }))
    loadData()
  }

  const handleUpdateModelAction = async (id: string, payload: LLMModel) => {
    await updateModel(id, payload)
    toast.success(t('config.toastModelCreated', { name: payload.name }))
    loadData()
  }

  const handleDeleteModelAction = (id: string) => {
    const m = models.find((m) => m.id === id)
    if (m) setDeleteModelTarget(m)
  }

  const confirmDeleteModel = async () => {
    if (!deleteModelTarget) return
    try {
      await deleteModel(deleteModelTarget.id)
      setDeleteModelTarget(null)
      loadData()
      toast.success(t('config.toastModelDeleted'))
    } catch (err) {
      toast.error((err as Error).message)
      setDeleteModelTarget(null)
    }
  }

  const handleToggleModelStatus = async (m: LLMModel) => {
    try {
      await updateModel(m.id, { ...m, enabled: !m.enabled })
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  if (loading) {
    return (
      <div className="text-sm font-mono text-muted-foreground p-6">
        {t('common.loading')}
      </div>
    )
  }

  return (
    <div className="space-y-6 pb-10">
      <LLMSection
        providers={providers}
        models={models}
        defaultModels={defaultModels}
        onSaveDefaults={handleSaveDefaults}
        onDefaultModelsChange={setDefaultModels}
        onCreateProvider={handleCreateProvider}
        onUpdateProvider={handleUpdateProviderAction}
        onDeleteProvider={handleDeleteProviderAction}
        onToggleProviderStatus={handleToggleProviderStatus}
        onSetProviderAsDefault={handleSetProviderAsDefault}
        onCreateModel={handleCreateModel}
        onUpdateModel={handleUpdateModelAction}
        onDeleteModel={handleDeleteModelAction}
        onToggleModelStatus={handleToggleModelStatus}
      />

      <ConfirmDialog
        open={!!deleteProviderTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteProviderTarget(null)
        }}
        title={t('config.deleteProviderTitle')}
        message={t('config.deleteProviderMsg', { id: deleteProviderTarget?.id || '' })}
        destructive
        onConfirm={confirmDeleteProvider}
        confirmLabel={t('config.deleteProviderTitle')}
      />
      <ConfirmDialog
        open={!!deleteModelTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteModelTarget(null)
        }}
        title={t('config.deleteModelTitle')}
        message={t('config.deleteModelMsg', { id: deleteModelTarget?.id || '' })}
        destructive
        onConfirm={confirmDeleteModel}
        confirmLabel={t('config.deleteModelTitle')}
      />
    </div>
  )
}

export default ModelsTab
