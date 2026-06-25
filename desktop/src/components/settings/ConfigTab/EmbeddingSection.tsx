import { Database, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { EmbeddingConfig } from '@/types'

interface EmbeddingSectionProps {
  config: EmbeddingConfig
  onChange: (config: EmbeddingConfig) => void
  onSave: () => void
  onAddProvider: () => void
  onRemoveProvider: (index: number) => void
  onUpdateProvider: (index: number, field: string, val: any) => void
  onAddModel: () => void
  onRemoveModel: (index: number) => void
  onUpdateModel: (index: number, field: string, val: any) => void
}

export function EmbeddingSection({
  config,
  onChange,
  onSave,
  onAddProvider,
  onRemoveProvider,
  onUpdateProvider,
  onAddModel,
  onRemoveModel,
  onUpdateModel,
}: EmbeddingSectionProps) {
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-8">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">Embedding (Vector Store) Settings</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            Embedding Settings: Configure vector search models and providers for semantic memory
            indexing. If enabled, memory entries will be vectorized for retrieval.
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          Save Embedding Settings
        </Button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="flex items-center gap-2">
          <Switch
            checked={config.enabled}
            onCheckedChange={(val) => onChange({ ...config, enabled: val })}
          />
          <span className="text-xs font-semibold text-foreground">
            Enable Permanent Memory / Vector Store
          </span>
        </div>
        <div className="flex flex-col gap-1.5">
          <Select
            label="Provider"
            value={config.provider || ''}
            onChange={(v) => onChange({ ...config, provider: v })}
            placeholder="none (default)"
            options={[
              { value: '', label: 'none (default)' },
              { value: 'none', label: 'none — BM25 + KG only' },
              { value: 'openai', label: 'openai — Remote API' },
            ]}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Min Similarity Threshold (0.0–1.0)
          </label>
          <Input
            type="number"
            step="0.01"
            min="0"
            max="1"
            value={config.minSimilarity ?? 0.65}
            onChange={(e) => onChange({ ...config, minSimilarity: Number(e.target.value) })}
          />
        </div>
      </div>

      {/* Embedding Providers Section */}
      {config.provider === 'openai' && (
        <>
          <div className="space-y-4 pt-4 border-t">
            <div className="flex items-center justify-between">
              <h4 className="text-sm font-semibold text-foreground">Embedding Providers</h4>
              <Button size="xs" variant="outline" onClick={onAddProvider}>
                <Plus className="h-3 w-3 mr-1" /> Add Provider
              </Button>
            </div>

            <div className="space-y-3">
              {(config.providers || []).map((prov, idx) => (
                <div
                  key={prov.id || idx}
                  className="p-4 border rounded-lg relative space-y-4 bg-muted/20"
                >
                  <button
                    type="button"
                    onClick={() => onRemoveProvider(idx)}
                    className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground font-mono">
                        Provider ID
                      </label>
                      <Input
                        type="text"
                        placeholder="e.g. local"
                        value={prov.id || ''}
                        onChange={(e) => onUpdateProvider(idx, 'id', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        Provider Name
                      </label>
                      <Input
                        type="text"
                        placeholder="e.g. Ollama"
                        value={prov.name || ''}
                        onChange={(e) => onUpdateProvider(idx, 'name', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        Base URL
                      </label>
                      <Input
                        type="text"
                        placeholder="http://localhost:11434"
                        value={prov.baseUrl || ''}
                        onChange={(e) => onUpdateProvider(idx, 'baseUrl', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        API Key (Direct)
                      </label>
                      <Input
                        type="password"
                        placeholder="sk-..."
                        value={prov.apiKey || ''}
                        onChange={(e) => onUpdateProvider(idx, 'apiKey', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground font-mono">
                        API Key Env Variable
                      </label>
                      <Input
                        type="text"
                        placeholder="e.g. OLLAMA_API_KEY"
                        value={prov.apiKeyEnv || ''}
                        onChange={(e) => onUpdateProvider(idx, 'apiKeyEnv', e.target.value)}
                      />
                    </div>
                    <div className="flex items-center gap-2 pt-6">
                      <Switch
                        checked={prov.enabled}
                        onCheckedChange={(val) => onUpdateProvider(idx, 'enabled', val)}
                      />
                      <span className="text-xs font-semibold text-foreground">Enabled</span>
                    </div>
                  </div>
                </div>
              ))}
              {(config.providers || []).length === 0 && (
                <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-xs">
                  No embedding providers defined.
                </div>
              )}
            </div>
          </div>

          {/* Embedding Models Section */}
          <div className="space-y-4 pt-4 border-t">
            <div className="flex items-center justify-between">
              <h4 className="text-sm font-semibold text-foreground">Embedding Models</h4>
              <Button size="xs" variant="outline" onClick={onAddModel}>
                <Plus className="h-3 w-3 mr-1" /> Add Model
              </Button>
            </div>

            <div className="space-y-3">
              {(config.models || []).map((mdl, idx) => (
                <div
                  key={mdl.id || idx}
                  className="p-4 border rounded-lg relative space-y-4 bg-muted/20"
                >
                  <button
                    type="button"
                    onClick={() => onRemoveModel(idx)}
                    className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground font-mono">
                        Model ID
                      </label>
                      <Input
                        type="text"
                        placeholder="e.g. nomic-embed-text"
                        value={mdl.id || ''}
                        onChange={(e) => onUpdateModel(idx, 'id', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1 font-mono">
                      <label className="text-xs font-semibold text-muted-foreground">
                        Provider ID
                      </label>
                      <Select
                        value={mdl.providerId || ''}
                        onChange={(v) => onUpdateModel(idx, 'providerId', v)}
                        placeholder="Select Provider"
                        options={[
                          { value: '', label: 'Select Provider' },
                          ...(config.providers || []).map((p) => ({
                            value: p.id,
                            label: `${p.name} (${p.id})`,
                          })),
                        ]}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        Model Name
                      </label>
                      <Input
                        type="text"
                        placeholder="e.g. Nomic Text Embeddings"
                        value={mdl.name || ''}
                        onChange={(e) => onUpdateModel(idx, 'name', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        Dimension Size
                      </label>
                      <Input
                        type="number"
                        value={mdl.dimension || 1024}
                        onChange={(e) => onUpdateModel(idx, 'dimension', Number(e.target.value))}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        Batch Size
                      </label>
                      <Input
                        type="number"
                        value={mdl.batchSize || 32}
                        onChange={(e) => onUpdateModel(idx, 'batchSize', Number(e.target.value))}
                      />
                    </div>
                    <div className="flex items-center gap-6 pt-6 flex-wrap">
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={mdl.normalize}
                          onCheckedChange={(val) => onUpdateModel(idx, 'normalize', val)}
                        />
                        <span className="text-xs font-semibold text-foreground">Normalize</span>
                      </div>
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={mdl.isDefault}
                          onCheckedChange={(val) => onUpdateModel(idx, 'isDefault', val)}
                        />
                        <span className="text-xs font-semibold text-foreground">
                          Is Default Model
                        </span>
                      </div>
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={mdl.enabled}
                          onCheckedChange={(val) => onUpdateModel(idx, 'enabled', val)}
                        />
                        <span className="text-xs font-semibold text-foreground">Enabled</span>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
              {(config.models || []).length === 0 && (
                <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-xs">
                  No embedding models defined.
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
