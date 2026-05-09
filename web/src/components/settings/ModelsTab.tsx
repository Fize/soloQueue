import { useState } from 'react';
import { useConfig } from '@/hooks/useConfig';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Select } from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Save, ChevronDown, ChevronRight, Plus, Trash2 } from 'lucide-react';
import type { LLMProvider, LLMModel, DefaultModelsConfig, EmbeddingConfig } from '@/types';

export function ModelsTab() {
  const { config, patch, saving, error } = useConfig();

  const [providers, setProviders] = useState<LLMProvider[] | null>(null);
  const [models, setModels] = useState<LLMModel[] | null>(null);
  const [defaultModels, setDefaultModels] = useState<DefaultModelsConfig | null>(null);
  const [embedding, setEmbedding] = useState<EmbeddingConfig | null>(null);
  const [expandedProvider, setExpandedProvider] = useState<string | null>(null);
  const [expandedModel, setExpandedModel] = useState<string | null>(null);
  const [expandedEmbedProvider, setExpandedEmbedProvider] = useState<string | null>(null);
  const [expandedEmbedModel, setExpandedEmbedModel] = useState<string | null>(null);

  if (config && !providers) {
    setProviders(config.providers.map((p) => ({ ...p, retry: { ...p.retry } })));
    setModels(config.models.map((m) => ({ ...m, generation: { ...m.generation }, thinking: { ...m.thinking } })));
    setDefaultModels({ ...config.defaultModels });
    setEmbedding({
      ...config.embedding,
      providers: config.embedding.providers.map((p) => ({ ...p })),
      models: config.embedding.models.map((m) => ({ ...m })),
    });
  }

  if (!config || !providers || !models || !defaultModels || !embedding) {
    return <div className="text-sm text-muted-foreground">Loading configuration...</div>;
  }

  const modelOptions = models.map((m) => ({ value: `${m.providerId}:${m.id}`, label: m.name }));
  const providerOptions = providers.map((p) => ({ value: p.id, label: p.name }));

  const handleSaveProviders = async () => {
    await patch({ providers });
  };

  const handleSaveModels = async () => {
    await patch({ models });
  };

  const handleSaveDefaultModels = async () => {
    await patch({ defaultModels });
  };

  const handleSaveEmbedding = async () => {
    await patch({ embedding });
  };

  const addProvider = () => {
    const id = `provider-${Date.now()}`;
    setProviders([
      ...providers,
      { id, name: '', baseUrl: '', apiKeyEnv: '', enabled: true, isDefault: false, timeoutMs: 600000, retry: { maxRetries: 3, initialDelayMs: 1000, maxDelayMs: 30000, backoffMultiplier: 2 } },
    ]);
    setExpandedProvider(`provider-${providers.length}`);
  };

  const removeProvider = (id: string) => {
    setProviders(providers.filter((p) => p.id !== id));
  };

  const updateProvider = (index: number, field: keyof LLMProvider, value: unknown) => {
    setProviders(providers.map((p, i) => (i === index ? { ...p, [field]: value } : p)));
  };

  const addModel = () => {
    const id = `model-${Date.now()}`;
    const providerId = providers[0]?.id || '';
    setModels([
      ...models,
      { id, providerId, name: '', contextWindow: 32768, enabled: true, generation: { temperature: 0, maxTokens: 16384 }, thinking: { enabled: false, reasoningEffort: '' } },
    ]);
    setExpandedModel(`model-${models.length}`);
  };

  const removeModel = (id: string) => {
    setModels(models.filter((m) => m.id !== id));
  };

  const updateModel = (index: number, field: string, value: unknown) => {
    setModels(models.map((m, i) => (i === index ? { ...m, [field]: value } : m)));
  };

  const addEmbedProvider = () => {
    const id = `embed-provider-${Date.now()}`;
    setEmbedding({
      ...embedding,
      providers: [...embedding.providers, { id, name: '', baseUrl: '', apiKeyEnv: '', enabled: false }],
    });
    setExpandedEmbedProvider(`ep-${embedding.providers.length}`);
  };

  const removeEmbedProvider = (id: string) => {
    setEmbedding({ ...embedding, providers: embedding.providers.filter((p) => p.id !== id) });
  };

  const addEmbedModel = () => {
    const id = `embed-model-${Date.now()}`;
    const providerId = embedding.providers[0]?.id || '';
    setEmbedding({
      ...embedding,
      models: [...embedding.models, { id, providerId, name: '', dimension: 768, batchSize: 32, normalize: true, enabled: false, isDefault: false }],
    });
    setExpandedEmbedModel(`em-${embedding.models.length}`);
  };

  const removeEmbedModel = (id: string) => {
    setEmbedding({ ...embedding, models: embedding.models.filter((m) => m.id !== id) });
  };

  return (
    <div className="space-y-6">
      {/* Providers */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-bold text-foreground">Providers</h3>
          <Button size="sm" variant="outline" onClick={addProvider}>
            <Plus className="mr-1 h-3 w-3" /> Add
          </Button>
        </div>
        <div className="space-y-2">
          {providers.map((provider, idx) => (
            <div key={`provider-${idx}`} className="rounded-md border-2 border-[#EEEEEE]">
              <button
                className="flex w-full items-center justify-between px-3 py-2 text-left"
                onClick={() => setExpandedProvider(expandedProvider === `provider-${idx}` ? null : `provider-${idx}`)}
              >
                <div className="flex items-center gap-2">
                  {expandedProvider === `provider-${idx}` ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                  <span className="text-sm font-medium text-foreground">{provider.name || provider.id}</span>
                  {provider.isDefault && <Badge variant="default" className="text-[10px]">Default</Badge>}
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant={provider.enabled ? 'secondary' : 'outline'} className="text-[10px]">
                    {provider.enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                  <button
                    onClick={(e) => { e.stopPropagation(); removeProvider(provider.id); }}
                    className="text-muted-foreground hover:text-destructive"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              </button>
              {expandedProvider === `provider-${idx}` && (
                <div className="grid gap-3 border-t-2 border-[#EEEEEE] px-3 py-3">
                  <div className="grid grid-cols-2 gap-3">
                    <Input label="ID" value={provider.id} onChange={(e) => updateProvider(idx, 'id', e.target.value)} />
                    <Input label="Name" value={provider.name} onChange={(e) => updateProvider(idx, 'name', e.target.value)} />
                  </div>
                  <Input label="Base URL" value={provider.baseUrl} onChange={(e) => updateProvider(idx, 'baseUrl', e.target.value)} />
                  <div className="grid grid-cols-2 gap-3">
                    <Input label="API Key Env" value={provider.apiKeyEnv} onChange={(e) => updateProvider(idx, 'apiKeyEnv', e.target.value)} />
                    <Input label="Timeout (ms)" type="number" value={provider.timeoutMs} onChange={(e) => updateProvider(idx, 'timeoutMs', Number(e.target.value))} />
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="flex items-center justify-between">
                      <Label>Enabled</Label>
                      <Switch checked={provider.enabled} onCheckedChange={(v) => updateProvider(idx, 'enabled', v)} />
                    </div>
                    <div className="flex items-center justify-between">
                      <Label>Default</Label>
                      <Switch checked={provider.isDefault} onCheckedChange={(v) => updateProvider(idx, 'isDefault', v)} />
                    </div>
                  </div>
                  <div className="grid grid-cols-4 gap-2 border-t-2 border-[#EEEEEE] pt-2">
                    <Input label="Max Retries" type="number" value={provider.retry.maxRetries} onChange={(e) => updateProvider(idx, 'retry', { ...provider.retry, maxRetries: Number(e.target.value) })} />
                    <Input label="Initial Delay (ms)" type="number" value={provider.retry.initialDelayMs} onChange={(e) => updateProvider(idx, 'retry', { ...provider.retry, initialDelayMs: Number(e.target.value) })} />
                    <Input label="Max Delay (ms)" type="number" value={provider.retry.maxDelayMs} onChange={(e) => updateProvider(idx, 'retry', { ...provider.retry, maxDelayMs: Number(e.target.value) })} />
                    <Input label="Backoff" type="number" step={0.1} value={provider.retry.backoffMultiplier} onChange={(e) => updateProvider(idx, 'retry', { ...provider.retry, backoffMultiplier: Number(e.target.value) })} />
                  </div>
                </div>
              )}
            </div>
          ))}
          {providers.length === 0 && <p className="py-3 text-center text-xs text-muted-foreground">No providers configured</p>}
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSaveProviders} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Providers'}
          </Button>
        </div>
      </div>

      {/* Models */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-bold text-foreground">Models</h3>
          <Button size="sm" variant="outline" onClick={addModel}>
            <Plus className="mr-1 h-3 w-3" /> Add
          </Button>
        </div>
        <div className="space-y-2">
          {models.map((model, idx) => (
            <div key={`model-${idx}`} className="rounded-md border-2 border-[#EEEEEE]">
              <button
                className="flex w-full items-center justify-between px-3 py-2 text-left"
                onClick={() => setExpandedModel(expandedModel === `model-${idx}` ? null : `model-${idx}`)}
              >
                <div className="flex items-center gap-2">
                  {expandedModel === `model-${idx}` ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                  <span className="text-sm font-medium text-foreground">{model.name || model.id}</span>
                  <span className="text-[10px] text-muted-foreground">({model.providerId})</span>
                </div>
                <div className="flex items-center gap-2">
                  {model.thinking.enabled && <Badge variant="default" className="text-[10px]">Thinking</Badge>}
                  <Badge variant={model.enabled ? 'secondary' : 'outline'} className="text-[10px]">
                    {model.enabled ? 'On' : 'Off'}
                  </Badge>
                  <button
                    onClick={(e) => { e.stopPropagation(); removeModel(model.id); }}
                    className="text-muted-foreground hover:text-destructive"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              </button>
              {expandedModel === `model-${idx}` && (
                <div className="grid gap-3 border-t-2 border-[#EEEEEE] px-3 py-3">
                  <div className="grid grid-cols-2 gap-3">
                    <Input label="ID" value={model.id} onChange={(e) => updateModel(idx, 'id', e.target.value)} />
                    <Input label="Name" value={model.name} onChange={(e) => updateModel(idx, 'name', e.target.value)} />
                  </div>
                  <div className="grid grid-cols-3 gap-3">
                    <Select label="Provider" options={providerOptions} value={model.providerId} onChange={(v) => updateModel(idx, 'providerId', v)} />
                    <Input label="API Model" value={model.apiModel || ''} onChange={(e) => updateModel(idx, 'apiModel', e.target.value)} />
                    <Input label="Context Window" type="number" value={model.contextWindow} onChange={(e) => updateModel(idx, 'contextWindow', Number(e.target.value))} />
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="flex items-center justify-between">
                      <Label>Enabled</Label>
                      <Switch checked={model.enabled} onCheckedChange={(v) => updateModel(idx, 'enabled', v)} />
                    </div>
                    <div className="flex items-center justify-between">
                      <Label>Thinking Mode</Label>
                      <Switch checked={model.thinking.enabled} onCheckedChange={(v) => updateModel(idx, 'thinking', { ...model.thinking, enabled: v })} />
                    </div>
                  </div>
                  <div className="grid grid-cols-3 gap-2 border-t-2 border-[#EEEEEE] pt-2">
                    <Input label="Temperature" type="number" step={0.1} value={model.generation.temperature} onChange={(e) => updateModel(idx, 'generation', { ...model.generation, temperature: Number(e.target.value) })} />
                    <Input label="Max Tokens" type="number" value={model.generation.maxTokens} onChange={(e) => updateModel(idx, 'generation', { ...model.generation, maxTokens: Number(e.target.value) })} />
                    {model.thinking.enabled && (
                      <Select
                        label="Reasoning Effort"
                        options={[{ value: '', label: 'None' }, { value: 'high', label: 'High' }, { value: 'max', label: 'Max' }]}
                        value={model.thinking.reasoningEffort}
                        onChange={(v) => updateModel(idx, 'thinking', { ...model.thinking, reasoningEffort: v })}
                      />
                    )}
                  </div>
                </div>
              )}
            </div>
          ))}
          {models.length === 0 && <p className="py-3 text-center text-xs text-muted-foreground">No models configured</p>}
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSaveModels} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Models'}
          </Button>
        </div>
      </div>

      {/* Default Models */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="mb-4 text-sm font-bold text-foreground">Default Model Assignments</h3>
        <div className="grid gap-3">
          {(Object.entries(defaultModels) as [keyof DefaultModelsConfig, string][]).map(([role, modelId]) => (
            <div key={role} className="grid grid-cols-[120px_1fr] items-center gap-3">
              <Label className="capitalize">{role}</Label>
              <Select
                options={[{ value: '', label: '— Not Set —' }, ...modelOptions]}
                value={modelId}
                onChange={(v) => setDefaultModels({ ...defaultModels, [role]: v })}
              />
            </div>
          ))}
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSaveDefaultModels} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Defaults'}
          </Button>
        </div>
      </div>

      {/* Embedding */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-bold text-foreground">Embedding</h3>
          <Switch checked={embedding.enabled} onCheckedChange={(v) => setEmbedding({ ...embedding, enabled: v })} />
        </div>

        {embedding.enabled && (
          <>
            {/* Embedding Providers */}
            <div className="mb-4 space-y-2">
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Providers</span>
                <Button size="sm" variant="outline" onClick={addEmbedProvider}>
                  <Plus className="mr-1 h-3 w-3" /> Add
                </Button>
              </div>
              {embedding.providers.map((ep, idx) => (
                <div key={`ep-${idx}`} className="rounded-md border-2 border-[#EEEEEE]">
                  <button
                    className="flex w-full items-center justify-between px-3 py-2 text-left"
                    onClick={() => setExpandedEmbedProvider(expandedEmbedProvider === `ep-${idx}` ? null : `ep-${idx}`)}
                  >
                    <div className="flex items-center gap-2">
                      {expandedEmbedProvider === `ep-${idx}` ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                      <span className="text-sm font-medium text-foreground">{ep.name || ep.id}</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <Badge variant={ep.enabled ? 'secondary' : 'outline'} className="text-[10px]">
                        {ep.enabled ? 'On' : 'Off'}
                      </Badge>
                      <button onClick={(e) => { e.stopPropagation(); removeEmbedProvider(ep.id); }} className="text-muted-foreground hover:text-destructive">
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </div>
                  </button>
                  {expandedEmbedProvider === `ep-${idx}` && (
                    <div className="grid gap-3 border-t-2 border-[#EEEEEE] px-3 py-3">
                      <div className="grid grid-cols-2 gap-3">
                        <Input label="ID" value={ep.id} onChange={(e) => setEmbedding({ ...embedding, providers: embedding.providers.map((p) => p.id === ep.id ? { ...p, id: e.target.value } : p) })} />
                        <Input label="Name" value={ep.name} onChange={(e) => setEmbedding({ ...embedding, providers: embedding.providers.map((p) => p.id === ep.id ? { ...p, name: e.target.value } : p) })} />
                      </div>
                      <Input label="Base URL" value={ep.baseUrl} onChange={(e) => setEmbedding({ ...embedding, providers: embedding.providers.map((p) => p.id === ep.id ? { ...p, baseUrl: e.target.value } : p) })} />
                      <div className="grid grid-cols-2 gap-3">
                        <Input label="API Key Env" value={ep.apiKeyEnv} onChange={(e) => setEmbedding({ ...embedding, providers: embedding.providers.map((p) => p.id === ep.id ? { ...p, apiKeyEnv: e.target.value } : p) })} />
                        <div className="flex items-center justify-between pt-5">
                          <Label>Enabled</Label>
                          <Switch checked={ep.enabled} onCheckedChange={(v) => setEmbedding({ ...embedding, providers: embedding.providers.map((p) => p.id === ep.id ? { ...p, enabled: v } : p) })} />
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>

            {/* Embedding Models */}
            <div className="space-y-2">
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Models</span>
                <Button size="sm" variant="outline" onClick={addEmbedModel}>
                  <Plus className="mr-1 h-3 w-3" /> Add
                </Button>
              </div>
              {embedding.models.map((em, idx) => (
                <div key={`em-${idx}`} className="rounded-md border-2 border-[#EEEEEE]">
                  <button
                    className="flex w-full items-center justify-between px-3 py-2 text-left"
                    onClick={() => setExpandedEmbedModel(expandedEmbedModel === `em-${idx}` ? null : `em-${idx}`)}
                  >
                    <div className="flex items-center gap-2">
                      {expandedEmbedModel === `em-${idx}` ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                      <span className="text-sm font-medium text-foreground">{em.name || em.id}</span>
                      {em.isDefault && <Badge variant="default" className="text-[10px]">Default</Badge>}
                    </div>
                    <div className="flex items-center gap-2">
                      <Badge variant={em.enabled ? 'secondary' : 'outline'} className="text-[10px]">
                        {em.enabled ? 'On' : 'Off'}
                      </Badge>
                      <button onClick={(e) => { e.stopPropagation(); removeEmbedModel(em.id); }} className="text-muted-foreground hover:text-destructive">
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </div>
                  </button>
                  {expandedEmbedModel === `em-${idx}` && (
                    <div className="grid gap-3 border-t-2 border-[#EEEEEE] px-3 py-3">
                      <div className="grid grid-cols-2 gap-3">
                        <Input label="ID" value={em.id} onChange={(e) => setEmbedding({ ...embedding, models: embedding.models.map((m) => m.id === em.id ? { ...m, id: e.target.value } : m) })} />
                        <Input label="Name" value={em.name} onChange={(e) => setEmbedding({ ...embedding, models: embedding.models.map((m) => m.id === em.id ? { ...m, name: e.target.value } : m) })} />
                      </div>
                      <div className="grid grid-cols-3 gap-3">
                        <Input label="Dimension" type="number" value={em.dimension} onChange={(e) => setEmbedding({ ...embedding, models: embedding.models.map((m) => m.id === em.id ? { ...m, dimension: Number(e.target.value) } : m) })} />
                        <Input label="Batch Size" type="number" value={em.batchSize} onChange={(e) => setEmbedding({ ...embedding, models: embedding.models.map((m) => m.id === em.id ? { ...m, batchSize: Number(e.target.value) } : m) })} />
                        <div className="flex items-center justify-between pt-5">
                          <Label>Normalize</Label>
                          <Switch checked={em.normalize} onCheckedChange={(v) => setEmbedding({ ...embedding, models: embedding.models.map((m) => m.id === em.id ? { ...m, normalize: v } : m) })} />
                        </div>
                      </div>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="flex items-center justify-between">
                          <Label>Enabled</Label>
                          <Switch checked={em.enabled} onCheckedChange={(v) => setEmbedding({ ...embedding, models: embedding.models.map((m) => m.id === em.id ? { ...m, enabled: v } : m) })} />
                        </div>
                        <div className="flex items-center justify-between">
                          <Label>Default</Label>
                          <Switch checked={em.isDefault} onCheckedChange={(v) => setEmbedding({ ...embedding, models: embedding.models.map((m) => m.id === em.id ? { ...m, isDefault: v } : m) })} />
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          </>
        )}

        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSaveEmbedding} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Embedding'}
          </Button>
          {error && <span className="text-[10px] text-destructive">{error}</span>}
        </div>
      </div>
    </div>
  );
}
