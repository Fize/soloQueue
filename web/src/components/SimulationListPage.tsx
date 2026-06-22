import { useEffect, useState, useRef, ChangeEvent, FormEvent, MouseEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Play, Plus, Sparkles, Trash2, Calendar, Users, AlertCircle, Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import type { SimulationState } from '@/types'

export function SimulationListPage() {
  const navigate = useNavigate()
  const [simulations, setSimulations] = useState<SimulationState[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Delete confirmation state
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)

  // Creation form state
  const [topic, setTopic] = useState('')
  const [seedText, setSeedText] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  // Advanced configuration state
  const [providers, setProviders] = useState<{ id: string; name: string }[]>([])
  const [models, setModels] = useState<{ id: string; name: string; providerId: string }[]>([])
  const [selectedModel, setSelectedModel] = useState('')
  const [selectedProvider, setSelectedProvider] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [simHours, setSimHours] = useState(48)
  const [enableReflection, setEnableReflection] = useState(true)
  const [timeScale, setTimeScale] = useState(600)
  const [tickIntervalMs, setTickIntervalMs] = useState(500)
  const [language, setLanguage] = useState('zh')
  const [maxWallClockMs, setMaxWallClockMs] = useState(18 * 60 * 1000)

  const fileInputRef = useRef<HTMLInputElement | null>(null)

  const fetchConfigOptions = async () => {
    try {
      const provRes = await fetch('/api/config/providers')
      if (provRes.ok) {
        const provData = await provRes.json()
        setProviders(provData || [])
      }
      const modelRes = await fetch('/api/config/models')
      if (modelRes.ok) {
        const modelData = await modelRes.json()
        setModels(modelData || [])
      }
      const simRes = await fetch('/api/config/simulation')
      if (simRes.ok) {
        const simData = await simRes.json()
        if (simData.enableReflection !== undefined) setEnableReflection(simData.enableReflection)
        if (simData.simulatedHours !== undefined) setSimHours(simData.simulatedHours)
        if (simData.defaultModelId !== undefined) setSelectedModel(simData.defaultModelId)
        if (simData.defaultProviderId !== undefined) setSelectedProvider(simData.defaultProviderId)
        if (simData.timeScale !== undefined) setTimeScale(simData.timeScale)
        if (simData.tickIntervalMs !== undefined) setTickIntervalMs(simData.tickIntervalMs)
        if (simData.language !== undefined) setLanguage(simData.language)
        if (simData.defaultMaxWallClockMs !== undefined)
          setMaxWallClockMs(simData.defaultMaxWallClockMs)
      }
    } catch (err) {
      console.error('Failed to load LLM configs', err)
      toast.error('Failed to load LLM configs')
    }
  }

  const MAX_FILE_SIZE = 50 * 1024 * 1024 // 50MB

  const handleFileUpload = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return

    if (file.size > MAX_FILE_SIZE) {
      setError(
        `File "${file.name}" is ${(file.size / 1024 / 1024).toFixed(1)}MB. Maximum allowed size is 50MB.`
      )
      // Reset the input so the user can re-select
      if (fileInputRef.current) fileInputRef.current.value = ''
      return
    }
    setError('')

    const reader = new FileReader()
    reader.onload = (event) => {
      const text = event.target?.result as string
      if (text) {
        setSeedText(text)
        if (!topic.trim()) {
          const suggestedTopic = file.name
            .replace(/\.[^/.]+$/, '') // remove extension
            .replace(/[-_]/g, ' ') // replace dashes/underscores with spaces
          setTopic(suggestedTopic)
        }
      }
    }
    reader.onerror = () => {
      setError(`Failed to read file "${file.name}". Please try again.`)
    }
    reader.readAsText(file)
  }

  const fetchSimulations = async () => {
    try {
      setLoading(true)
      const res = await fetch('/api/simulations')
      if (!res.ok) {
        throw new Error('Failed to fetch simulations')
      }
      const data = await res.json()
      const mapped = (data || []).map((sim: any) => ({
        ...sim,
        id: sim.config?.id || sim.run_id || sim.id,
        personas: sim.config?.personas || [],
        round: sim.current_round || 0,
        simulated_hours: sim.config?.simulated_hours || 48,
        time_scale: sim.config?.time_scale || 600,
        enable_reflection: sim.config?.enable_reflection || false,
      }))
      setSimulations(mapped)
      setError(null)
    } catch (err: any) {
      setError(err.message || 'An error occurred')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchSimulations()
    fetchConfigOptions()
  }, [])

  const handleCreateFromSeed = async (e: FormEvent) => {
    e.preventDefault()
    if (!seedText.trim()) return

    try {
      setCreating(true)
      setCreateError(null)

      const res = await fetch('/api/simulations/from-seed', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          seed_text: seedText,
          topic: topic.trim() || undefined,
          persona_count: 0, // auto-detect
          model_id: selectedModel || undefined,
          provider_id: selectedProvider || undefined,
          max_wall_clock_ms: maxWallClockMs > 0 ? maxWallClockMs : undefined,
          simulated_hours: simHours > 0 ? simHours : undefined,
          time_scale: timeScale,
          tick_interval_ms: tickIntervalMs,
          enable_reflection: enableReflection || undefined,
          language: language,
        }),
      })

      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to generate simulation from seed')
      }

      const data = await res.json()
      navigate(`/simulations/${data.simulation_id}`)
    } catch (err: any) {
      setCreateError(err.message || 'Failed to create simulation')
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (id: string, e: MouseEvent) => {
    e.stopPropagation()
    setDeleteTarget(id)
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      const res = await fetch(`/api/simulations/${deleteTarget}`, {
        method: 'DELETE',
      })
      if (!res.ok) {
        throw new Error('Failed to delete simulation')
      }
      setSimulations((prev) => prev.filter((s) => s.id !== deleteTarget))
      toast.success('Simulation deleted')
    } catch (err: any) {
      toast.error(err.message || 'Failed to delete simulation')
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }

  const getStatusBadgeClass = (status: string) => {
    switch (status) {
      case 'running':
        return 'bg-emerald-500/10 text-emerald-500 dark:text-emerald-400 border border-emerald-500/25'
      case 'completed':
        return 'bg-primary/10 text-primary border border-primary/25'
      case 'failed':
        return 'bg-rose-500/10 text-rose-500 dark:text-rose-400 border border-rose-500/25'
      default:
        return 'bg-muted-foreground/10 text-muted-foreground border border-muted-foreground/25'
    }
  }

  return (
    <>
      <div className="flex h-full flex-col overflow-y-auto bg-background p-6 text-foreground">
        <div className="mx-auto w-full max-w-6xl min-w-[320px]">
          {/* Header */}
          <div className="mb-8 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
            <div>
              <h1 className="text-2xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/75 bg-clip-text text-transparent">
                Multi-Agent Simulations
              </h1>
              <p className="mt-1 text-sm text-muted-foreground">
                Create autonomous virtual sandboxes to predict social dynamics and analyze complex
                topics.
              </p>
            </div>
          </div>

          {/* Main Grid */}
          <div className="grid gap-8 lg:grid-cols-[1fr_360px]">
            {/* Main Column: Form to create from Seed */}
            <div className="min-w-0">
              <div className="mx-auto max-w-2xl rounded-xl border border-border bg-card/45 p-6 backdrop-blur-md">
                <div className="mb-4 flex items-center gap-2">
                  <Sparkles className="h-5 w-5 text-primary" />
                  <h2 className="text-lg font-semibold text-foreground">Auto-Generate Sandbox</h2>
                </div>
                <p className="mb-5 text-xs text-muted-foreground leading-relaxed">
                  Inject a seed document to automatically extract key entities and generate virtual
                  agent personas with opposing stances.
                </p>

                <form onSubmit={handleCreateFromSeed} className="space-y-4">
                  <div>
                    <div className="flex items-center justify-between mb-1.5">
                      <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                        Seed Material *
                      </label>
                      <button
                        type="button"
                        onClick={() => fileInputRef.current?.click()}
                        className="text-[10px] text-primary hover:underline font-mono flex items-center gap-1 cursor-pointer"
                      >
                        Import File
                      </button>
                      <input
                        type="file"
                        ref={fileInputRef}
                        onChange={handleFileUpload}
                        accept=".txt,.md,.json,.toml,.csv"
                        className="hidden"
                      />
                    </div>
                    <Textarea
                      required
                      rows={6}
                      placeholder="Paste context, news article, or policy details here..."
                      value={seedText}
                      onChange={(e) => setSeedText(e.target.value)}
                      className="resize-none font-sans"
                    />
                  </div>

                  {/* Topic auto-detection hint */}
                  {topic && (
                    <p className="text-[11px] text-muted-foreground/60 font-mono -mt-2">
                      Topic auto-detected: <span className="text-foreground/80">{topic}</span>
                    </p>
                  )}

                  {/* Custom LLM Config (collapsed) */}
                  <div className="border-t border-border pt-3">
                    <button
                      type="button"
                      onClick={() => setShowAdvanced(!showAdvanced)}
                      className="flex w-full items-center justify-between text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono hover:text-foreground transition-colors cursor-pointer select-none"
                    >
                      <span>Custom LLM Config</span>
                      <span className="text-xs">{showAdvanced ? '▼' : '▶'}</span>
                    </button>

                    {showAdvanced && (
                      <div className="mt-3 grid grid-cols-2 gap-3">
                        <div>
                          <Select
                            label="Provider"
                            value={selectedProvider}
                            onChange={(v) => {
                              setSelectedProvider(v)
                              setSelectedModel('')
                            }}
                            placeholder="(Default Fast Provider)"
                            options={[
                              { value: '', label: '(Default Fast Provider)' },
                              ...providers.map((p) => ({ value: p.id, label: p.name })),
                            ]}
                          />
                        </div>
                        <div>
                          <Select
                            label="Model"
                            value={selectedModel}
                            onChange={setSelectedModel}
                            placeholder="(Default Fast Model)"
                            options={[
                              { value: '', label: '(Default Fast Model)' },
                              ...models
                                .filter(
                                  (m) => !selectedProvider || m.providerId === selectedProvider
                                )
                                .map((m) => ({
                                  value: m.id,
                                  label: m.name,
                                })),
                            ]}
                          />
                        </div>
                      </div>
                    )}
                  </div>

                  {createError && (
                    <div className="flex items-start gap-2 rounded-lg bg-rose-500/10 p-3 text-xs text-rose-500 dark:text-rose-400 border border-rose-500/20">
                      <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                      <span>{createError}</span>
                    </div>
                  )}

                  <button
                    type="submit"
                    disabled={creating}
                    className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary hover:bg-primary/90 disabled:bg-primary/50 px-4 py-2.5 text-sm font-semibold text-primary-foreground transition-all shadow-md shadow-primary/5 cursor-pointer disabled:cursor-not-allowed"
                  >
                    {creating ? (
                      <>
                        <Loader2 className="h-4 w-4 animate-spin" />
                        Extracting & Generating...
                      </>
                    ) : (
                      <>
                        <Plus className="h-4 w-4" />
                        Generate & Launch
                      </>
                    )}
                  </button>
                </form>
              </div>
            </div>

            {/* Right Column: Simulations List */}
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-foreground">Simulation History</h2>
                <button
                  onClick={fetchSimulations}
                  className="text-xs text-muted-foreground hover:text-foreground font-mono transition-colors"
                >
                  Refresh List
                </button>
              </div>

              {loading ? (
                <div className="flex h-64 items-center justify-center rounded-xl border border-border bg-card/20">
                  <Loader2 className="h-8 w-8 animate-spin text-primary" />
                </div>
              ) : error ? (
                <div className="flex h-64 flex-col items-center justify-center rounded-xl border border-border bg-card/20 p-6 text-center text-rose-500 dark:text-rose-400">
                  <AlertCircle className="mb-2 h-8 w-8" />
                  <p className="text-sm font-semibold">{error}</p>
                  <button
                    onClick={fetchSimulations}
                    className="mt-4 rounded-lg bg-muted hover:bg-muted/80 px-4 py-1.5 text-xs text-foreground transition-colors"
                  >
                    Try Again
                  </button>
                </div>
              ) : simulations.length === 0 ? (
                <div className="flex h-64 flex-col items-center justify-center rounded-xl border border-dashed border-border bg-card/10 p-6 text-center text-muted-foreground">
                  <Play className="mb-2 h-8 w-8 text-muted-foreground/60" />
                  <p className="text-sm">No simulations found.</p>
                  <p className="text-xs text-muted-foreground/85 mt-1">
                    Use the panel on the left to spawn your first multi-agent debate sandbox!
                  </p>
                </div>
              ) : (
                <div className="grid gap-4 sm:grid-cols-2">
                  {simulations.map((sim) => (
                    <div
                      key={sim.id}
                      onClick={() => navigate(`/simulations/${sim.id}`)}
                      className="group relative flex flex-col justify-between rounded-xl border border-border bg-card/25 p-5 hover:bg-card/45 hover:border-border/80 transition-all cursor-pointer shadow-sm hover:shadow-md"
                    >
                      <div className="mb-4">
                        <div className="flex items-center justify-between mb-2">
                          <span
                            className={`rounded px-2 py-0.5 text-[10px] font-bold uppercase ${getStatusBadgeClass(sim.status)}`}
                          >
                            {sim.status}
                          </span>
                          <button
                            onClick={(e) => handleDelete(sim.id, e)}
                            className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-rose-500 transition-opacity p-1"
                            title="Delete simulation"
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>

                        <h3 className="font-semibold text-foreground group-hover:text-primary transition-colors line-clamp-1">
                          {sim.config.topic}
                        </h3>
                        <p className="mt-1.5 text-xs text-muted-foreground font-mono text-[10px]">
                          ID: {sim.id.slice(0, 8)}...
                        </p>
                      </div>

                      <div className="flex items-center justify-between border-t border-border/60 pt-3 text-[11px] text-muted-foreground font-mono">
                        <div className="flex items-center gap-1.5">
                          <Users className="h-3.5 w-3.5" />
                          <span>{sim.config?.personas?.length || 0} Agents</span>
                        </div>
                        {sim.started_at && (
                          <div className="flex items-center gap-1.5">
                            <Calendar className="h-3.5 w-3.5" />
                            <span>{new Date(sim.started_at).toLocaleDateString()}</span>
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
        title="Delete Simulation"
        message="Are you sure you want to delete this simulation? This action cannot be undone."
        destructive
        onConfirm={confirmDelete}
        confirmLabel="Delete"
        loading={deleting}
      />
    </>
  )
}
