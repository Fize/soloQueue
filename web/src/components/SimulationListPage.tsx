import { useEffect, useState, useRef, ChangeEvent, FormEvent, MouseEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Play, Plus, Sparkles, Trash2, Calendar, Users, AlertCircle, Loader2 } from 'lucide-react'
import type { SimulationState } from '@/types'

export function SimulationListPage() {
  const navigate = useNavigate()
  const [simulations, setSimulations] = useState<SimulationState[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Creation form state
  const [topic, setTopic] = useState('')
  const [seedText, setSeedText] = useState('')
  const [autoPersonaCount, setAutoPersonaCount] = useState(true)
  const [personaCount, setPersonaCount] = useState(3)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  // Advanced configuration state
  const [providers, setProviders] = useState<{ id: string; name: string }[]>([])
  const [models, setModels] = useState<{ id: string; name: string; providerId: string }[]>([])
  const [selectedModel, setSelectedModel] = useState('')
  const [selectedProvider, setSelectedProvider] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [maxWallClockMin, setMaxWallClockMin] = useState(18)
  const [simHours, setSimHours] = useState(168)
  const [timeScale, setTimeScale] = useState(600)
  const [enableReflection, setEnableReflection] = useState(true)
  const [dbPath, setDbPath] = useState('')

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
        if (simData.dbPath !== undefined) setDbPath(simData.dbPath)
        if (simData.enableReflection !== undefined) setEnableReflection(simData.enableReflection)
        if (simData.simulatedHours !== undefined) setSimHours(simData.simulatedHours)
        if (simData.defaultMaxWallClockMs !== undefined) {
          setMaxWallClockMin(Math.round(simData.defaultMaxWallClockMs / 60000))
        }
      }
    } catch (err) {
      console.error('Failed to load LLM configs', err)
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

      // Save global simulation settings first (like DB path)
      try {
        await fetch('/api/config/simulation', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            dbPath: dbPath,
            enableReflection: enableReflection,
            simulatedHours: simHours,
            defaultMaxWallClockMs: maxWallClockMin ? maxWallClockMin * 60 * 1000 : undefined,
          }),
        })
      } catch (err) {
        console.error('Failed to update simulation config before launch', err)
      }

      const res = await fetch('/api/simulations/from-seed', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          seed_text: seedText,
          topic: topic.trim() || undefined,
          persona_count: autoPersonaCount ? 0 : personaCount,
          model_id: selectedModel || undefined,
          provider_id: selectedProvider || undefined,
          max_wall_clock_ms: maxWallClockMin ? maxWallClockMin * 60 * 1000 : undefined,
          simulated_hours: simHours || undefined,
          time_scale: timeScale || undefined,
          tick_interval_ms: 500,
          enable_reflection: enableReflection || undefined,
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
    if (!confirm('Are you sure you want to delete this simulation?')) return

    try {
      const res = await fetch(`/api/simulations/${id}`, {
        method: 'DELETE',
      })
      if (!res.ok) {
        throw new Error('Failed to delete simulation')
      }
      setSimulations((prev) => prev.filter((s) => s.id !== id))
    } catch (err: any) {
      alert(err.message || 'Failed to delete simulation')
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
    <div className="flex h-full flex-col overflow-y-auto bg-background p-6 text-foreground">
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
      <div className="grid gap-8 lg:grid-cols-3">
        {/* Left Column: Form to create from Seed */}
        <div className="lg:col-span-1">
          <div className="rounded-xl border border-border bg-card/45 p-5 backdrop-blur-md">
            <div className="mb-4 flex items-center gap-2">
              <Sparkles className="h-5 w-5 text-primary" />
              <h2 className="text-lg font-semibold text-foreground">Auto-Generate Sandbox</h2>
            </div>
            <p className="mb-4 text-xs text-muted-foreground leading-relaxed">
              Inject a seed document (article, proposal, prompt) to automatically extract key
              entities and generate virtual agent personas with opposing stances.
            </p>

            <form onSubmit={handleCreateFromSeed} className="space-y-4">
              <div>
                <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider mb-1.5 font-mono">
                  Topic (Optional)
                </label>
                <input
                  type="text"
                  placeholder="e.g., Remote Work Efficiency"
                  value={topic}
                  onChange={(e) => setTopic(e.target.value)}
                  className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all"
                />
              </div>

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
                <textarea
                  required
                  rows={6}
                  placeholder="Paste context, news article, or policy details here..."
                  value={seedText}
                  onChange={(e) => setSeedText(e.target.value)}
                  className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all resize-none font-sans"
                />
              </div>

              <div>
                <div className="flex items-center justify-between mb-1.5 font-mono">
                  <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider">
                    Number of Agent Personas
                  </label>
                  <label className="flex items-center gap-1.5 text-[10px] text-primary cursor-pointer select-none">
                    <input
                      type="checkbox"
                      checked={autoPersonaCount}
                      onChange={(e) => setAutoPersonaCount(e.target.checked)}
                      className="rounded border-border bg-background text-primary focus:ring-primary h-3 w-3 accent-primary"
                    />
                    Auto-detect
                  </label>
                </div>

                {autoPersonaCount ? (
                  <div className="rounded-lg border border-dashed border-border bg-muted/20 px-3 py-2.5 text-[11px] text-muted-foreground leading-normal">
                    Factions and personas will be inferred automatically from the seed material
                    details (usually 3 to 5 agents).
                  </div>
                ) : (
                  <>
                    <input
                      type="range"
                      min={2}
                      max={5}
                      value={personaCount}
                      onChange={(e) => setPersonaCount(parseInt(e.target.value))}
                      className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                    />
                    <div className="flex justify-between text-[10px] text-muted-foreground font-mono mt-1">
                      <span>2 Agents</span>
                      <span>5 Agents ({personaCount} selected)</span>
                    </div>
                  </>
                )}
              </div>

              {/* Advanced Options Toggle */}
              <div className="border-t border-border pt-4">
                <button
                  type="button"
                  onClick={() => setShowAdvanced(!showAdvanced)}
                  className="flex w-full items-center justify-between text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono hover:text-foreground transition-colors cursor-pointer select-none"
                >
                  <span>Advanced Options</span>
                  <span className="text-xs">{showAdvanced ? '▼' : '▶'}</span>
                </button>

                {showAdvanced && (
                  <div className="mt-4 space-y-4 border-l-2 border-primary/20 pl-3">
                    {/* Provider & Model */}
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="block text-[9px] font-bold text-muted-foreground uppercase tracking-wider mb-1 font-mono">
                          Provider / 服务商
                        </label>
                        <select
                          value={selectedProvider}
                          onChange={(e) => {
                            setSelectedProvider(e.target.value)
                            setSelectedModel('')
                          }}
                          className="w-full rounded-lg border border-border bg-background px-2 py-1.5 text-xs text-foreground focus:border-primary focus:outline-none transition-all cursor-pointer"
                        >
                          <option value="">(Default Fast Provider)</option>
                          {providers.map((p) => (
                            <option key={p.id} value={p.id}>
                              {p.name}
                            </option>
                          ))}
                        </select>
                        <p className="text-[9px] text-muted-foreground/80 mt-1 leading-normal">
                          角色默认使用的 LLM 接口服务商。
                        </p>
                      </div>

                      <div>
                        <label className="block text-[9px] font-bold text-muted-foreground uppercase tracking-wider mb-1 font-mono">
                          Model / 模型
                        </label>
                        <select
                          value={selectedModel}
                          onChange={(e) => setSelectedModel(e.target.value)}
                          className="w-full rounded-lg border border-border bg-background px-2 py-1.5 text-xs text-foreground focus:border-primary focus:outline-none transition-all cursor-pointer"
                        >
                          <option value="">(Default Fast Model)</option>
                          {models
                            .filter((m) => !selectedProvider || m.providerId === selectedProvider)
                            .map((m) => (
                              <option key={m.id} value={m.id}>
                                {m.name}
                              </option>
                            ))}
                        </select>
                        <p className="text-[9px] text-muted-foreground/80 mt-1 leading-normal">
                          角色默认对话使用的语言大模型。
                        </p>
                      </div>
                    </div>

                    {/* Wall Clock & Simulated Hours */}
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="block text-[9px] font-bold text-muted-foreground uppercase tracking-wider mb-1 font-mono">
                          Max Clock: {maxWallClockMin} min / 最长时间
                        </label>
                        <input
                          type="range"
                          min={1}
                          max={30}
                          value={maxWallClockMin}
                          onChange={(e) => setMaxWallClockMin(parseInt(e.target.value))}
                          className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                        />
                        <p className="text-[9px] text-muted-foreground/80 mt-1 leading-normal">
                          仿真实际运行的物理时钟超时限制。
                        </p>
                      </div>

                      <div>
                        <label className="block text-[9px] font-bold text-muted-foreground uppercase tracking-wider mb-1 font-mono">
                          Sim Hours: {simHours}h / 模拟时长
                        </label>
                        <input
                          type="range"
                          min={6}
                          max={168}
                          step={6}
                          value={simHours}
                          onChange={(e) => {
                            const val = parseInt(e.target.value) || 48
                            setSimHours(val)
                            setMaxWallClockMin(Math.round((val * 5) / 48))
                          }}
                          className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                        />
                        <p className="text-[9px] text-muted-foreground/80 mt-1 leading-normal">
                          模拟世界内的时间跨度。
                        </p>
                      </div>
                    </div>

                    {/* Time Scale & Enable Reflection */}
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="block text-[9px] font-bold text-muted-foreground uppercase tracking-wider mb-1 font-mono">
                          Time Scale: 1s = {Math.round(timeScale / 60)}min / 时间倍速
                        </label>
                        <select
                          value={timeScale}
                          onChange={(e) => setTimeScale(parseInt(e.target.value))}
                          className="w-full rounded-lg border border-border bg-background px-2 py-1.5 text-xs text-foreground focus:border-primary focus:outline-none transition-all cursor-pointer"
                        >
                          <option value={60}>1s = 1min</option>
                          <option value={300}>1s = 5min</option>
                          <option value={600}>1s = 10min</option>
                          <option value={1800}>1s = 30min</option>
                          <option value={3600}>1s = 1h</option>
                        </select>
                        <p className="text-[9px] text-muted-foreground/80 mt-1 leading-normal">
                          现实时间与模拟时间的映射关系。
                        </p>
                      </div>

                      <div>
                        <label className="block text-[9px] font-bold text-muted-foreground uppercase tracking-wider mb-1 font-mono">
                          Reflection / 反思
                        </label>
                        <div className="flex items-center gap-2 mt-1">
                          <button
                            type="button"
                            onClick={() => setEnableReflection(!enableReflection)}
                            className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                              enableReflection ? 'bg-primary' : 'bg-muted'
                            }`}
                          >
                            <span
                              className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${
                                enableReflection ? 'translate-x-[18px]' : 'translate-x-[3px]'
                              }`}
                            />
                          </button>
                          <span className="text-[10px] text-muted-foreground">
                            {enableReflection ? 'On' : 'Off'}
                          </span>
                        </div>
                        <p className="text-[9px] text-muted-foreground/80 mt-1 leading-normal">
                          让 agent 定期反思自己的经历，生成高层次见解。会增加 LLM 调用量。
                        </p>
                      </div>
                    </div>

                    {/* Database Path */}
                    <div className="border-t border-border/40 pt-3">
                      <label className="block text-[9px] font-bold text-muted-foreground uppercase tracking-wider mb-1 font-mono">
                        Database Path / 数据库路径
                      </label>
                      <input
                        type="text"
                        placeholder="e.g. ~/.soloqueue/simulation.db"
                        value={dbPath}
                        onChange={(e) => setDbPath(e.target.value)}
                        className="w-full rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:outline-none transition-all"
                      />
                      <p className="text-[9px] text-muted-foreground/80 mt-1 leading-normal">
                        仿真记录持久化的 SQLite 数据库路径。若为空将使用默认位置。
                      </p>
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

        {/* Right Columns: Simulations List */}
        <div className="lg:col-span-2 space-y-4">
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
  )
}
