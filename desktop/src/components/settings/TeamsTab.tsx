import { useState, useEffect, useCallback, useRef } from 'react'
import { createPortal } from 'react-dom'
import {
  listTeams,
  createTeam,
  updateTeam,
  deleteTeam,
  listAgents,
  createAgent,
  updateAgent,
  deleteAgent,
  listProjects,
  listModels,
  getAvailableMCPServers,
  getSkills,
} from '@/lib/api'
import type { TeamResponse, AgentResponse, Project, LLMModel } from '@/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Users, Plus, Pencil, Trash2, Loader2, Eye, FileText as FileTextIcon, X } from 'lucide-react'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'

// ─── MultiSelect Component ──────────────────────────────────────────────────

interface MultiSelectProps {
  label?: string
  placeholder?: string
  options: string[]
  selected: string[]
  onChange: (selected: string[]) => void
  builtinNames?: Set<string>
}

function MultiSelect({ label, placeholder, options, selected, onChange, builtinNames }: MultiSelectProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [search, setSearch] = useState('')
  const containerRef = useRef<HTMLDivElement>(null)
  const [dropdownPos, setDropdownPos] = useState<{ top: number; left: number; width: number } | null>(null)
  
  const handleRemove = (item: string) => {
    onChange(selected.filter(x => x !== item))
  }
  
  const handleSelect = (item: string) => {
    if (!selected.includes(item)) {
      onChange([...selected, item])
    }
    setSearch('')
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && search.trim()) {
      e.preventDefault()
      e.stopPropagation()
      const newItem = search.trim()
      if (!selected.includes(newItem)) {
        onChange([...selected, newItem])
      }
      setSearch('')
    }
  }

  useEffect(() => {
    const handleOutsideClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false)
      }
    }
    document.addEventListener('mousedown', handleOutsideClick)
    return () => document.removeEventListener('mousedown', handleOutsideClick)
  }, [])

  useEffect(() => {
    if (!isOpen || !containerRef.current) {
      setDropdownPos(null)
      return
    }
    const updatePos = () => {
      if (!containerRef.current) return
      const rect = containerRef.current.getBoundingClientRect()
      setDropdownPos({ top: rect.bottom + 4, left: rect.left, width: rect.width })
    }
    updatePos()
    window.addEventListener('scroll', updatePos, true)
    window.addEventListener('resize', updatePos)
    return () => {
      window.removeEventListener('scroll', updatePos, true)
      window.removeEventListener('resize', updatePos)
    }
  }, [isOpen])

  const filteredOptions = options.filter(
    opt => opt.toLowerCase().includes(search.toLowerCase()) && !selected.includes(opt)
  )

  const dropdown = isOpen && dropdownPos && (filteredOptions.length > 0 || search.trim()) && createPortal(
    <div
      className="fixed z-[9999] max-h-48 overflow-y-auto rounded-md border border-border bg-popover text-popover-foreground shadow-md animate-in fade-in-0 duration-100"
      style={{ top: dropdownPos.top, left: dropdownPos.left, width: dropdownPos.width }}
    >
      <div className="p-1">
        {filteredOptions.map(opt => (
          <button
            key={opt}
            type="button"
            onClick={() => handleSelect(opt)}
            className="w-full text-left px-2.5 py-1.5 text-xs rounded hover:bg-accent hover:text-accent-foreground cursor-default select-none outline-none transition-colors flex items-center justify-between"
          >
            <span>{opt}</span>
            {builtinNames?.has(opt) && (
              <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 text-muted-foreground">builtin</Badge>
            )}
          </button>
        ))}
        {search.trim() && !options.includes(search.trim()) && !selected.includes(search.trim()) && (
          <button
            type="button"
            onClick={() => handleSelect(search.trim())}
            className="w-full text-left px-2.5 py-1.5 text-xs rounded hover:bg-accent hover:text-accent-foreground cursor-default select-none outline-none italic text-muted-foreground font-medium transition-colors"
          >
            Add "{search.trim()}"...
          </button>
        )}
      </div>
    </div>,
    document.body
  )

  return (
    <div ref={containerRef} className="flex flex-col gap-1.5 text-left w-full">
      {label && <Label className="text-xs font-semibold text-muted-foreground">{label}</Label>}
      <div className="min-h-10 w-full rounded-md border border-border bg-card px-3 py-1.5 text-xs flex flex-wrap gap-1.5 items-center focus-within:ring-1 focus-within:ring-primary/40 focus-within:border-primary transition-all">
        {selected.map(item => (
          <Badge key={item} variant="secondary" className="flex items-center gap-1 py-0.5 pl-2 pr-1 border border-border">
            <span>{item}</span>
            {builtinNames?.has(item) && (
              <span className="text-[9px] text-muted-foreground font-medium">builtin</span>
            )}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                handleRemove(item)
              }}
              className="rounded-full outline-none hover:bg-muted p-0.5 text-muted-foreground hover:text-foreground transition-colors"
            >
              <X className="h-2.5 w-2.5" />
            </button>
          </Badge>
        ))}
        <input
          type="text"
          placeholder={selected.length === 0 ? placeholder : ''}
          value={search}
          onChange={e => {
            setSearch(e.target.value)
            setIsOpen(true)
          }}
          onFocus={() => setIsOpen(true)}
          onKeyDown={handleKeyDown}
          className="flex-1 bg-transparent border-0 outline-none placeholder:text-muted-foreground min-w-[80px] p-0 text-xs text-foreground focus:ring-0 focus:outline-none"
        />
      </div>
      {dropdown}
    </div>
  )
}

// ─── Team Dialog ────────────────────────────────────────────────────────────

interface TeamDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: () => void
  editTeam?: TeamResponse | null
}

function TeamDialog({ open, onOpenChange, onSave, editTeam }: TeamDialogProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [allProjects, setAllProjects] = useState<Project[]>([])
  const [associatedProjects, setAssociatedProjects] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { t } = useTranslation()

  const isEdit = !!editTeam

  useEffect(() => {
    if (open) {
      listProjects().then(setAllProjects).catch(console.error)
      if (editTeam) {
        setName(editTeam.name)
        setDescription(editTeam.description || '')
        setAssociatedProjects(editTeam.projects || [])
      } else {
        setName('')
        setDescription('')
        setAssociatedProjects([])
      }
      setError(null)
    }
  }, [open, editTeam])

  const handleSave = useCallback(async () => {
    if (!name.trim()) {
      setError(t('teams.teamNameRequired'))
      return
    }

    setSaving(true)
    setError(null)
    try {
      if (isEdit) {
        await updateTeam(editTeam!.name, {
          description: description || undefined,
          projects: associatedProjects,
        })
      } else {
        await createTeam({
          name: name.trim(),
          description: description || undefined,
          projects: associatedProjects,
        })
      }
      onSave()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('teams.failedToSaveTeam'))
    } finally {
      setSaving(false)
    }
  }, [name, description, associatedProjects, isEdit, editTeam, t, onSave, onOpenChange])

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
        e.preventDefault()
        handleSave()
      }
    }
    if (open) {
      window.addEventListener('keydown', handleKeyDown)
    }
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [open, handleSave])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md w-[95vw] max-h-[90vh] flex flex-col overflow-hidden">
        <DialogHeader className="shrink-0">
          <div className="flex items-center gap-2">
            <DialogTitle className="text-sm font-bold text-foreground">
              {isEdit ? t('teams.editTeam') : t('teams.createTeam')}
            </DialogTitle>
            {isEdit && <Badge variant="outline">{editTeam?.name}</Badge>}
          </div>
          <DialogDescription className="text-xs">
            {isEdit
              ? t('teams.updateTeamDesc', { name: editTeam?.name })
              : t('teams.createTeamDesc')}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto min-h-0 my-2 space-y-4 text-left">
          {!isEdit && (
            <Input
              label={t('common.name')}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('teams.teamDescPlaceholder')}
              className="text-xs"
            />
          )}

          <div className="flex flex-col gap-1.5">
            <Label className="text-xs font-semibold text-muted-foreground">{t('teams.teamDescription')}</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              placeholder={t('teams.teamDescPlaceholder')}
              className="text-xs"
            />
          </div>

          <div className="flex flex-col gap-1.5 pt-1">
            <Label className="text-xs font-semibold text-muted-foreground">{t('teams.associatedProjects')}</Label>
            <div className="border border-border rounded-md p-2.5 max-h-[200px] overflow-y-auto space-y-2 bg-muted/10">
              {allProjects.length === 0 ? (
                <p className="text-xs text-muted-foreground italic p-2 text-center">
                  {t('teams.noProjects')}
                </p>
              ) : (
                <div className="grid grid-cols-1 gap-2">
                  {allProjects.map((p) => {
                    const checked = associatedProjects.includes(p.id)
                    return (
                      <div
                        key={p.id}
                        onClick={() => {
                          if (checked) {
                            setAssociatedProjects((prev) => prev.filter((id) => id !== p.id))
                          } else {
                            setAssociatedProjects((prev) => [...prev, p.id])
                          }
                        }}
                        className={`flex items-start gap-2.5 p-2 rounded-lg border cursor-pointer select-none transition-all ${
                          checked
                            ? 'border-primary bg-primary/5 ring-1 ring-primary/20'
                            : 'border-border bg-card hover:bg-muted/40'
                        }`}
                      >
                        <Checkbox
                          checked={checked}
                          onCheckedChange={() => {}} // Controlled by parent div click
                          className="mt-0.5"
                        />
                        <div className="flex flex-col min-w-0">
                          <span className="text-xs font-medium text-foreground truncate">{p.name}</span>
                          {p.description && (
                            <span className="text-[10px] text-muted-foreground truncate mt-0.5 leading-tight">
                              {p.description}
                            </span>
                          )}
                        </div>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          </div>
        </div>

        {error && <p className="text-xs text-destructive text-left shrink-0">{error}</p>}

        <DialogFooter className="gap-2 shrink-0">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={saving} className="text-xs">
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSave} size="sm" disabled={saving} className="text-xs">
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
                {t('common.saving')}
              </>
            ) : isEdit ? (
              t('teams.saveChanges')
            ) : (
              t('teams.createTeamBtn')
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── Agent Dialog ───────────────────────────────────────────────────────────

interface AgentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: () => void
  editAgent?: AgentResponse | null
  teams: TeamResponse[]
}

function AgentDialog({ open, onOpenChange, onSave, editAgent, teams }: AgentDialogProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [teamName, setTeamName] = useState('')
  const [isLeader, setIsLeader] = useState(false)
  const [model, setModel] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [permission, setPermission] = useState(true)
  const [mcpServers, setMcpServers] = useState<string[]>([])
  const [skillIds, setSkillIds] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { t } = useTranslation()

  const [promptTab, setPromptTab] = useState<'edit' | 'preview'>('preview')
  const [mcpOptions, setMcpOptions] = useState<{ name: string; source: string; command?: string }[]>([])
  const [builtinMCPNames, setBuiltinMCPNames] = useState<Set<string>>(new Set())
  const [skillOptions, setSkillOptions] = useState<string[]>([])
  const [modelOptions, setModelOptions] = useState<LLMModel[]>([])
  const [selectedProviderFilter, setSelectedProviderFilter] = useState('all')
  const [showModelDropdown, setShowModelDropdown] = useState(false)
  const modelContainerRef = useRef<HTMLDivElement>(null)
  const [modelDropdownPos, setModelDropdownPos] = useState<{ top: number; left: number; width: number } | null>(null)

  const isEdit = !!editAgent

  useEffect(() => {
    if (open) {
      setPromptTab('preview')
      setSelectedProviderFilter('all')
      if (editAgent) {
        setName(editAgent.name)
        setDescription(editAgent.description || '')
        setTeamName(editAgent.team_name || '')
        setIsLeader(editAgent.is_leader)
        setModel(editAgent.model || '')
        setSystemPrompt(editAgent.system_prompt || '')
        setPermission(editAgent.permission)
        setMcpServers(editAgent.mcp_servers || [])
        setSkillIds(editAgent.skill_ids || [])
      } else {
        setName('')
        setDescription('')
        setTeamName(teams[0]?.name || '')
        setIsLeader(false)
        setModel('')
        setSystemPrompt('')
        setPermission(true)
        setMcpServers([])
        setSkillIds([])
      }
      setError(null)

      // Fetch autocomplete options
      getSkills()
        .then((res) => setSkillOptions(res.skills.map((s) => s.id)))
        .catch(console.error)
      
      getAvailableMCPServers()
        .then((res) => {
          setMcpOptions(res.servers.map(s => ({ name: s.name, source: s.source, command: s.command })))
          setBuiltinMCPNames(new Set(res.servers.filter(s => s.source === 'builtin').map(s => s.name)))
        })
        .catch(console.error)

      listModels()
        .then(setModelOptions)
        .catch(console.error)
    }
  }, [open, editAgent, teams])

  useEffect(() => {
    const handleOutsideClick = (e: MouseEvent) => {
      if (modelContainerRef.current && !modelContainerRef.current.contains(e.target as Node)) {
        setShowModelDropdown(false)
      }
    }
    document.addEventListener('mousedown', handleOutsideClick)
    return () => document.removeEventListener('mousedown', handleOutsideClick)
  }, [])

  useEffect(() => {
    if (!showModelDropdown || !modelContainerRef.current) {
      setModelDropdownPos(null)
      return
    }
    const updatePos = () => {
      if (!modelContainerRef.current) return
      const rect = modelContainerRef.current.getBoundingClientRect()
      setModelDropdownPos({ top: rect.bottom + 4, left: rect.left, width: rect.width })
    }
    updatePos()
    window.addEventListener('scroll', updatePos, true)
    window.addEventListener('resize', updatePos)
    return () => {
      window.removeEventListener('scroll', updatePos, true)
      window.removeEventListener('resize', updatePos)
    }
  }, [showModelDropdown])

  const handleSave = useCallback(async () => {
    if (!name.trim()) {
      setError(t('teams.agentNameRequired'))
      return
    }
    if (!teamName) {
      setError(t('teams.teamRequired'))
      return
    }

    setSaving(true)
    setError(null)
    try {
      if (isEdit) {
        await updateAgent(editAgent!.name, {
          description: description || undefined,
          team_name: teamName,
          is_leader: isLeader,
          model: model || undefined,
          system_prompt: systemPrompt || undefined,
          permission,
          mcp_servers: mcpServers.length > 0 ? mcpServers : undefined,
          skill_ids: skillIds.length > 0 ? skillIds : undefined,
        })
      } else {
        await createAgent({
          name: name.trim(),
          description: description || undefined,
          team_name: teamName,
          is_leader: isLeader,
          model: model || undefined,
          system_prompt: systemPrompt || undefined,
          permission,
          mcp_servers: mcpServers.length > 0 ? mcpServers : undefined,
          skill_ids: skillIds.length > 0 ? skillIds : undefined,
        })
      }
      onSave()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('teams.failedToSaveAgent'))
    } finally {
      setSaving(false)
    }
  }, [name, description, teamName, isLeader, model, systemPrompt, permission, mcpServers, skillIds, isEdit, editAgent, t, onSave, onOpenChange])

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
        e.preventDefault()
        handleSave()
      }
    }
    if (open) {
      window.addEventListener('keydown', handleKeyDown)
    }
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [open, handleSave])

  const teamOptions = teams.map((team) => ({ value: team.name, label: team.name }))
  const providers = ['all', ...Array.from(new Set(modelOptions.map(m => m.providerId)))]
  const filteredModelOptions = modelOptions.filter(opt => {
    const matchesSearch = opt.id.toLowerCase().includes(model.toLowerCase()) || 
                          opt.name.toLowerCase().includes(model.toLowerCase()) ||
                          opt.providerId.toLowerCase().includes(model.toLowerCase())
    const matchesProvider = selectedProviderFilter === 'all' || opt.providerId === selectedProviderFilter
    return matchesSearch && matchesProvider
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl w-[95vw] max-h-[90vh] flex flex-col overflow-hidden">
        <DialogHeader className="shrink-0">
          <div className="flex items-center gap-2.5">
            <DialogTitle className="text-sm font-bold text-foreground">
              {isEdit ? t('teams.editAgent') : t('teams.createAgent')}
            </DialogTitle>
            {isEdit && <Badge variant="outline">{editAgent?.name}</Badge>}
          </div>
          <DialogDescription className="text-xs">
            {isEdit ? t('teams.editAgentDesc') : t('teams.createAgentDesc')}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto min-h-0 my-2 space-y-4 text-left">
          {/* Base Settings */}
          {!isEdit && (
            <Input
              label={t('common.name')}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('teams.agentNamePlaceholder')}
              className="text-xs"
            />
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Select label={t('teams.team')} options={teamOptions} value={teamName} onChange={setTeamName} className="text-xs" />
            
            <div ref={modelContainerRef} className="flex flex-col gap-1.5 text-left w-full">
              <Label className="text-xs font-semibold text-muted-foreground">{t('teams.modelOverride')}</Label>
              <div>
                <Input
                  value={model}
                  onChange={(e) => {
                    setModel(e.target.value)
                    setShowModelDropdown(true)
                  }}
                  onFocus={() => setShowModelDropdown(true)}
                  placeholder={t('teams.modelOverridePlaceholder')}
                  className="text-xs"
                />
              </div>
            </div>
          </div>

          {showModelDropdown && modelDropdownPos && (filteredModelOptions.length > 0 || providers.length > 1) && createPortal(
            <div
              className="fixed z-[9999] max-h-56 overflow-y-auto rounded-md border border-border bg-popover text-popover-foreground shadow-md animate-in fade-in-0 duration-100 flex flex-col"
              style={{ top: modelDropdownPos.top, left: modelDropdownPos.left, width: modelDropdownPos.width }}
            >
              {providers.length > 2 && (
                <div className="flex gap-1 p-1.5 border-b border-border overflow-x-auto bg-muted/20 shrink-0 select-none">
                  {providers.map(p => {
                    const isSelected = selectedProviderFilter === p
                    return (
                      <button
                        key={p}
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation()
                          setSelectedProviderFilter(p)
                        }}
                        className={`px-2 py-0.5 text-[9px] font-semibold rounded-full border transition-all shrink-0 cursor-pointer ${
                          isSelected
                            ? 'bg-primary border-primary text-white'
                            : 'bg-card border-border text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        {p.toUpperCase()}
                      </button>
                    )
                  })}
                </div>
              )}
              <div className="p-1 overflow-y-auto">
                {filteredModelOptions.length === 0 ? (
                  <div className="text-[10px] text-muted-foreground text-center py-2 italic">
                    No models found
                  </div>
                ) : (
                  filteredModelOptions.map((opt) => (
                    <button
                      key={opt.id}
                      type="button"
                      onClick={() => {
                        setModel(opt.id)
                        setShowModelDropdown(false)
                      }}
                      className="w-full px-2.5 py-1.5 text-xs rounded hover:bg-accent hover:text-accent-foreground cursor-default select-none outline-none transition-colors flex items-center justify-between gap-2"
                    >
                      <span className="truncate">{opt.id}</span>
                      <span className="text-[9px] font-medium text-muted-foreground bg-muted/40 px-1 py-0.2 rounded border border-border/40 shrink-0">
                        {opt.providerId}
                      </span>
                    </button>
                  ))
                )}
              </div>
            </div>,
            document.body
          )}

          <div className="flex flex-col gap-1.5">
            <Label className="text-xs font-semibold text-muted-foreground">{t('teams.agentDescription')}</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
              placeholder={t('teams.agentDescPlaceholder')}
              className="text-xs"
            />
          </div>

          {/* Switches in visual cards */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="flex flex-col gap-2 rounded-lg border border-border p-3 bg-muted/5">
              <div className="flex items-center justify-between">
                <Label
                  className="text-xs font-semibold cursor-pointer text-foreground"
                  htmlFor="is-leader-switch"
                >
                  {t('teams.isAgentLeader')}
                </Label>
                <Switch id="is-leader-switch" checked={isLeader} onCheckedChange={setIsLeader} />
              </div>
              <p className="text-[10px] text-muted-foreground leading-normal">
                {t('teams.isAgentLeaderDesc')}
              </p>
            </div>

            <div className="flex flex-col gap-2 rounded-lg border border-border p-3 bg-muted/5">
              <div className="flex items-center justify-between">
                <Label
                  className="text-xs font-semibold cursor-pointer text-foreground"
                  htmlFor="permission-switch"
                >
                  {t('teams.bypassConfirm')}
                </Label>
                <Switch
                  id="permission-switch"
                  checked={permission}
                  onCheckedChange={setPermission}
                />
              </div>
              <p className="text-[10px] text-muted-foreground leading-normal">
                {t('teams.bypassConfirmDesc')}
              </p>
            </div>
          </div>

          {/* Multi-Select Selectors */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <MultiSelect
              label={t('teams.selectMcp')}
              placeholder={t('teams.mcpServersPlaceholder')}
              options={mcpOptions.map(o => o.name)}
              selected={mcpServers}
              onChange={setMcpServers}
              builtinNames={builtinMCPNames}
            />
            <MultiSelect
              label={t('teams.selectSkills')}
              placeholder={t('teams.skillIdsPlaceholder')}
              options={skillOptions}
              selected={skillIds}
              onChange={setSkillIds}
            />
          </div>

          {/* System Prompt & Markdown Preview */}
          <div className="flex flex-col gap-3 min-h-[300px] pt-2">
            <Tabs
              value={promptTab}
              onValueChange={(v: string) => setPromptTab(v as 'edit' | 'preview')}
              className="flex-grow flex flex-col"
            >
              <div className="flex items-center justify-between">
                <Label className="text-xs font-semibold text-muted-foreground">{t('teams.systemPrompt')}</Label>
                <TabsList className="bg-muted/60 p-0.5 rounded-md border border-border">
                  <TabsTrigger
                    value="edit"
                    className="flex items-center gap-1 rounded-[4px] px-2.5 py-1 text-xs font-medium"
                  >
                    <FileTextIcon className="h-3 w-3" />
                    {t('teams.edit')}
                  </TabsTrigger>
                  <TabsTrigger
                    value="preview"
                    className="flex items-center gap-1 rounded-[4px] px-2.5 py-1 text-xs font-medium"
                  >
                    <Eye className="h-3 w-3" />
                    {t('teams.preview')}
                  </TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="edit" className="flex-1 flex flex-col min-h-[260px] mt-2">
                <Textarea
                  value={systemPrompt}
                  onChange={(e) => setSystemPrompt(e.target.value)}
                  className="flex-grow min-h-[260px] font-mono text-xs w-full"
                  placeholder={t('teams.systemPromptPlaceholder')}
                  spellCheck={false}
                />
              </TabsContent>
              <TabsContent
                value="preview"
                className="flex-1 mt-2 min-h-[260px] max-h-[350px] overflow-y-auto rounded-md border border-border bg-muted/5 p-3 text-xs text-foreground prose prose-sm dark:prose-invert"
              >
                {systemPrompt.trim() ? (
                  <MarkdownPreview content={systemPrompt} />
                ) : (
                  <span className="text-xs text-muted-foreground italic">
                    {t('teams.noPromptYet')}
                  </span>
                )}
              </TabsContent>
            </Tabs>
          </div>
        </div>

        {error && <p className="text-xs text-destructive text-left shrink-0">{error}</p>}

        <DialogFooter className="gap-2 shrink-0">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={saving} className="text-xs">
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSave} size="sm" disabled={saving} className="text-xs">
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
                {t('common.saving')}
              </>
            ) : isEdit ? (
              t('teams.saveChanges')
            ) : (
              t('teams.createAgentBtn')
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── Main Component ─────────────────────────────────────────────────────────

export default function TeamsTab() {
  const [teams, setTeams] = useState<TeamResponse[]>([])
  const [agents, setAgents] = useState<AgentResponse[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const { t } = useTranslation()

  // Filter agents by selected team
  const [selectedTeam, setSelectedTeam] = useState<string | null>(null)
  const [deleteTeamTarget, setDeleteTeamTarget] = useState<TeamResponse | null>(null)
  const [deleteAgentTarget, setDeleteAgentTarget] = useState<AgentResponse | null>(null)

  // Team dialog state
  const [teamDialogOpen, setTeamDialogOpen] = useState(false)
  const [editingTeam, setEditingTeam] = useState<TeamResponse | null>(null)

  // Agent dialog state
  const [agentDialogOpen, setAgentDialogOpen] = useState(false)
  const [editingAgent, setEditingAgent] = useState<AgentResponse | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const [teamsData, agentsData, projectsData] = await Promise.all([
        listTeams(),
        listAgents(),
        listProjects(),
      ])
      setTeams(teamsData)
      setAgents(agentsData)
      setProjects(projectsData)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('teams.failedToLoad'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  // ── Team handlers ──────────────────────────────────────────────────────

  const handleCreateTeam = () => {
    setEditingTeam(null)
    setTeamDialogOpen(true)
  }

  const handleEditTeam = (team: TeamResponse) => {
    setEditingTeam(team)
    setTeamDialogOpen(true)
  }

  const handleDeleteTeam = async (team: TeamResponse) => {
    setDeleteTeamTarget(team)
  }

  const confirmDeleteTeam = async () => {
    if (!deleteTeamTarget) return
    try {
      await deleteTeam(deleteTeamTarget.name)
      if (selectedTeam === deleteTeamTarget.name) {
        setSelectedTeam(null)
      }
      setDeleteTeamTarget(null)
      await fetchData()
      toast.success(t('teams.teamDeleted'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('teams.failedToDeleteTeam'))
      setDeleteTeamTarget(null)
    }
  }

  const handleTeamSaved = () => {
    fetchData()
  }

  // ── Agent handlers ─────────────────────────────────────────────────────

  const handleCreateAgent = () => {
    setEditingAgent(null)
    setAgentDialogOpen(true)
  }

  const handleEditAgent = (agent: AgentResponse) => {
    setEditingAgent(agent)
    setAgentDialogOpen(true)
  }

  const handleDeleteAgent = async (agent: AgentResponse) => {
    setDeleteAgentTarget(agent)
  }

  const confirmDeleteAgent = async () => {
    if (!deleteAgentTarget) return
    try {
      await deleteAgent(deleteAgentTarget.name)
      setDeleteAgentTarget(null)
      await fetchData()
      toast.success(t('teams.agentDeleted'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('teams.failedToDeleteAgent'))
      setDeleteAgentTarget(null)
    }
  }

  const handleAgentSaved = () => {
    fetchData()
  }

  // ── Filter ─────────────────────────────────────────────────────────────

  const filteredAgents = selectedTeam ? agents.filter((a) => a.team_name === selectedTeam) : agents

  const getTeamAgentCount = (teamName: string) =>
    agents.filter((a) => a.team_name === teamName).length

  // ── Render ─────────────────────────────────────────────────────────────

  if (loading) {
    return <div className="text-sm text-muted-foreground">{t('common.loading')}</div>
  }

  return (
    <div className="space-y-6">
      {/* ── Teams Section ──────────────────────────────────────────────── */}
      <div className="border rounded-lg bg-card shadow-sm">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div className="flex items-center gap-2">
            <Users className="h-4 w-4 text-foreground" />
            <h3 className="text-sm font-bold text-foreground">{t('teams.teams')}</h3>
            <Badge variant="secondary" className="text-[10px]">
              {teams.length}
            </Badge>
          </div>
          <Button size="sm" onClick={handleCreateTeam} className="gap-1">
            <Plus className="h-3.5 w-3.5" />
            {t('teams.createTeamAction')}
          </Button>
        </div>

        {teams.length === 0 ? (
          <div className="px-5 py-6 text-center">
            <p className="text-sm text-muted-foreground">{t('teams.noTeamsYet')}</p>
          </div>
        ) : (
          <div className="divide-y divide-border">
            {teams.map((team) => {
              const count = getTeamAgentCount(team.name)
              const isActive = selectedTeam === team.name
              return (
                <div
                  key={team.name}
                  className={`px-5 py-3 flex items-center justify-between gap-3 transition-colors cursor-pointer hover:bg-muted/30 ${
                    isActive ? 'bg-muted/50' : ''
                  }`}
                  onClick={() => setSelectedTeam(isActive ? null : team.name)}
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground truncate">
                        {team.name}
                      </span>
                      <Badge variant="outline" className="text-[10px]">
                        {t('teams.agentCount', { count })}
                      </Badge>
                    </div>
                    {team.description && (
                      <p className="text-xs text-muted-foreground truncate mt-0.5">
                        {team.description}
                      </p>
                    )}
                    {team.projects && team.projects.length > 0 && (
                      <div className="flex gap-1.5 mt-1.5 flex-wrap">
                        {team.projects.map((projId) => {
                          const proj = projects.find((p) => p.id === projId)
                          const displayName = proj ? proj.name : projId
                          return (
                            <Badge
                              key={projId}
                              variant="secondary"
                              className="text-[9px] px-1.5 py-0.5 h-4"
                            >
                              {displayName}
                            </Badge>
                          )
                        })}
                      </div>
                    )}
                  </div>
                  <div
                    className="flex items-center gap-1 shrink-0"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      onClick={() => handleEditTeam(team)}
                      title={t('teams.editTeamTooltip')}
                    >
                      <Pencil className="h-3 w-3" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      onClick={() => handleDeleteTeam(team)}
                      title={t('teams.deleteTeamTooltip')}
                      className="text-destructive hover:text-destructive"
                    >
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* ── Agents Section ─────────────────────────────────────────────── */}
      <div className="border rounded-lg bg-card shadow-sm">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div className="flex items-center gap-2">
            <Users className="h-4 w-4 text-foreground" />
            <h3 className="text-sm font-bold text-foreground">{t('teams.agents')}</h3>
            <Badge variant="secondary" className="text-[10px]">
              {filteredAgents.length}
            </Badge>
            {selectedTeam && (
              <Badge
                variant="primary"
                className="text-[10px] cursor-pointer"
                onClick={() => setSelectedTeam(null)}
              >
                {t('teams.teamFilter', { name: selectedTeam })} ✕
              </Badge>
            )}
          </div>
          <Button size="sm" onClick={handleCreateAgent} className="gap-1">
            <Plus className="h-3.5 w-3.5" />
            {t('teams.createAgentAction')}
          </Button>
        </div>

        {filteredAgents.length === 0 ? (
          <div className="px-5 py-6 text-center">
            <p className="text-sm text-muted-foreground">
              {selectedTeam ? t('teams.noAgentsInTeam') : t('teams.noAgentsYet')}
            </p>
          </div>
        ) : (
          <div className="divide-y divide-border">
            {filteredAgents.map((agent) => (
              <div key={agent.name} className="px-5 py-3 flex items-center justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-sm font-medium text-foreground truncate">
                      {agent.name}
                    </span>
                    <Badge variant="outline" className="text-[10px]">
                      {t('teams.agentTeamBadge', { name: agent.team_name })}
                    </Badge>
                    {agent.is_leader && (
                      <Badge variant="primary" className="text-[10px]">
                        {t('teams.leader')}
                      </Badge>
                    )}
                    {agent.permission && (
                      <Badge variant="success" className="text-[10px]">
                        {t('teams.bypass')}
                      </Badge>
                    )}
                  </div>
                  {agent.description && (
                    <p className="text-xs text-muted-foreground truncate mt-0.5">
                      {agent.description}
                    </p>
                  )}
                  {agent.model && (
                    <p className="text-[10px] text-muted-foreground/60 mt-0.5">
                      {t('teams.model', { name: agent.model })}
                    </p>
                  )}
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => handleEditAgent(agent)}
                    title={t('teams.editAgentTooltip')}
                  >
                    <Pencil className="h-3 w-3" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => handleDeleteAgent(agent)}
                    title={t('teams.deleteAgentTooltip')}
                    className="text-destructive hover:text-destructive"
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* ── Dialogs ────────────────────────────────────────────────────── */}
      <TeamDialog
        open={teamDialogOpen}
        onOpenChange={setTeamDialogOpen}
        onSave={handleTeamSaved}
        editTeam={editingTeam}
      />

      <AgentDialog
        open={agentDialogOpen}
        onOpenChange={setAgentDialogOpen}
        onSave={handleAgentSaved}
        editAgent={editingAgent}
        teams={teams}
      />
      <ConfirmDialog
        open={!!deleteTeamTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTeamTarget(null)
        }}
        title={t('teams.deleteTeam')}
        message={t('teams.deleteTeamConfirmMsg', { name: deleteTeamTarget?.name ?? '' })}
        destructive
        onConfirm={confirmDeleteTeam}
        confirmLabel={t('teams.deleteTeam')}
      />
      <ConfirmDialog
        open={!!deleteAgentTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteAgentTarget(null)
        }}
        title={t('teams.deleteAgent')}
        message={t('teams.deleteAgentConfirmMsg', { name: deleteAgentTarget?.name ?? '' })}
        destructive
        onConfirm={confirmDeleteAgent}
        confirmLabel={t('teams.deleteAgent')}
      />
    </div>
  )
}
