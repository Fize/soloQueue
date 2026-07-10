import { useState, useEffect, useCallback } from 'react'
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
} from '@/lib/api'
import type { TeamResponse, AgentResponse, CreateTeamRequest, Project } from '@/types'
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
import { Users, Plus, Pencil, Trash2, Loader2, Eye, FileText as FileTextIcon } from 'lucide-react'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'

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
  const [workspacesJson, setWorkspacesJson] = useState('')
  const [allProjects, setAllProjects] = useState<Project[]>([])
  const [associatedProjects, setAssociatedProjects] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { t } = useTranslation()

  const [teamTab, setTeamTab] = useState<'edit' | 'preview'>('preview')

  const isEdit = !!editTeam

  useEffect(() => {
    if (open) {
      setTeamTab('preview')
      listProjects().then(setAllProjects).catch(console.error)
      if (editTeam) {
        setName(editTeam.name)
        setDescription(editTeam.description || '')
        setWorkspacesJson(
          editTeam.workspaces?.length ? JSON.stringify(editTeam.workspaces, null, 2) : ''
        )
        setAssociatedProjects(editTeam.projects || [])
      } else {
        setName('')
        setDescription('')
        setWorkspacesJson('')
        setAssociatedProjects([])
      }
      setError(null)
    }
  }, [open, editTeam])

  const handleSave = async () => {
    if (!name.trim()) {
      setError(t('teams.teamNameRequired'))
      return
    }

    // Parse workspaces JSON if provided
    let workspaces: CreateTeamRequest['workspaces'] = undefined
    if (workspacesJson.trim()) {
      try {
        workspaces = JSON.parse(workspacesJson)
        if (!Array.isArray(workspaces)) {
          setError(t('teams.workspacesMustBeArray'))
          return
        }
      } catch {
        setError(t('teams.invalidWorkspacesJSON'))
        return
      }
    }

    setSaving(true)
    setError(null)
    try {
      if (isEdit) {
        await updateTeam(editTeam!.name, {
          description: description || undefined,
          workspaces,
          projects: associatedProjects,
        })
      } else {
        await createTeam({
          name: name.trim(),
          description: description || undefined,
          workspaces,
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
  }

  // Generate workspaces Markdown preview
  let workspacesPreviewMD = '### ' + t('teams.workspacesConfigured') + '\n\n'
  if (workspacesJson.trim()) {
    try {
      const parsed = JSON.parse(workspacesJson)
      if (Array.isArray(parsed) && parsed.length > 0) {
        parsed.forEach((ws: any, idx: number) => {
          const wsName = ws.name || `${t('teams.workspacesConfig')} #${idx + 1}`
          const wsPath = ws.path || '*No path set*'
          workspacesPreviewMD += `- **${wsName}**: \`${wsPath}\`\n`
          if (ws.autoWork?.enabled) {
            workspacesPreviewMD += `  - *AutoWork*: ` + t('teams.cooldown', { cooldown: ws.autoWork.initialCooldownMinutes, max: ws.autoWork.maxIntervalsPerDay }) + '\n'
          }
        })
      } else {
        workspacesPreviewMD += '*' + t('teams.noWorkspacesConfiguredDesc') + '*'
      }
    } catch {
      workspacesPreviewMD =
        '⚠️ **' + t('teams.invalidJSONFormat') + '**\n\n' + t('teams.switchToEdit')
    }
  } else {
    workspacesPreviewMD += '*' + t('teams.noWorkspacesConfigured') + '*'
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl w-[95vw] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <DialogTitle>{isEdit ? t('teams.editTeam') : t('teams.createTeam')}</DialogTitle>
            {isEdit && <Badge variant="outline">{editTeam?.name}</Badge>}
          </div>
          <DialogDescription>
            {isEdit
              ? t('teams.updateTeamDesc', { name: editTeam?.name })
              : t('teams.createTeamDesc')}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col md:flex-row gap-6 my-2 text-left">
          {/* Left Column: Info */}
          <div className="flex-1 space-y-4">
            {!isEdit && (
              <Input
                label={t('common.name')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('teams.teamDescPlaceholder')}
              />
            )}

            <div className="flex flex-col gap-1.5">
              <Label>{t('teams.teamDescription')}</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={4}
                placeholder={t('teams.teamDescPlaceholder')}
              />
            </div>

            <div className="flex flex-col gap-1.5 pt-2">
              <Label className="font-semibold">{t('teams.associatedProjects')}</Label>
              <div className="border border-border rounded-md p-3 max-h-[180px] overflow-y-auto space-y-2 bg-muted/10">
                {allProjects.length === 0 ? (
                  <p className="text-xs text-muted-foreground italic">
                    {t('teams.noProjects')}
                  </p>
                ) : (
                  allProjects.map((p) => {
                    const checked = associatedProjects.includes(p.id)
                    return (
                      <label
                        key={p.id}
                        className="flex items-start gap-2.5 text-xs text-foreground cursor-pointer select-none"
                      >
                        <Checkbox
                          checked={checked}
                          onCheckedChange={(val) => {
                            if (val) {
                              setAssociatedProjects((prev) => [...prev, p.id])
                            } else {
                              setAssociatedProjects((prev) => prev.filter((id) => id !== p.id))
                            }
                          }}
                        />
                        <div className="flex flex-col">
                          <span className="font-medium">{p.name}</span>
                          {p.description && (
                            <span className="text-[10px] text-muted-foreground">
                              {p.description}
                            </span>
                          )}
                        </div>
                      </label>
                    )
                  })
                )}
              </div>
            </div>
          </div>

          {/* Right Column: Workspaces JSON + Preview */}
          <div className="flex-1 flex flex-col gap-3 min-h-[300px]">
            <Tabs
              value={teamTab}
              onValueChange={(v: string) => setTeamTab(v as 'edit' | 'preview')}
            >
              <div className="flex items-center justify-between">
                <Label className="font-semibold">{t('teams.workspacesConfig')}</Label>
                <TabsList className="bg-muted/60 p-0.5 rounded-md border border-border">
                  <TabsTrigger
                    value="edit"
                    className="flex items-center gap-1 rounded-[4px] px-2.5 py-1 text-xs font-medium"
                  >
                    <FileTextIcon className="h-3 w-3" />
                    {t('teams.editJSON')}
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

              <TabsContent value="edit" className="flex-1 flex flex-col min-h-[220px]">
                <div className="flex flex-col gap-1.5 h-full">
                  <Textarea
                    value={workspacesJson}
                    onChange={(e) => setWorkspacesJson(e.target.value)}
                    className="flex-1 min-h-[220px] font-mono text-xs"
                    placeholder={t('teams.editJSONPlaceholder')}
                    spellCheck={false}
                  />
                  <p className="text-[10px] text-muted-foreground/80 leading-normal">
                    {t('teams.jsonArrayHelp')}
                  </p>
                </div>
              </TabsContent>
              <TabsContent
                value="preview"
                className="flex-1 min-h-[220px] max-h-[300px] overflow-y-auto rounded-md border border-border bg-muted/5 p-3 text-sm text-foreground prose prose-sm dark:prose-invert"
              >
                <MarkdownPreview content={workspacesPreviewMD} />
              </TabsContent>
            </Tabs>
          </div>
        </div>

        {error && <p className="text-xs text-destructive text-left">{error}</p>}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
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
  const [mcpServersInput, setMcpServersInput] = useState('')
  const [skillIdsInput, setSkillIdsInput] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { t } = useTranslation()

  const [promptTab, setPromptTab] = useState<'edit' | 'preview'>('preview')

  const isEdit = !!editAgent

  useEffect(() => {
    if (open) {
      setPromptTab('preview')
      if (editAgent) {
        setName(editAgent.name)
        setDescription(editAgent.description || '')
        setTeamName(editAgent.team_name || '')
        setIsLeader(editAgent.is_leader)
        setModel(editAgent.model || '')
        setSystemPrompt(editAgent.system_prompt || '')
        setPermission(editAgent.permission)
        setMcpServersInput((editAgent.mcp_servers || []).join(', '))
        setSkillIdsInput((editAgent.skill_ids || []).join(', '))
      } else {
        setName('')
        setDescription('')
        setTeamName(teams[0]?.name || '')
        setIsLeader(false)
        setModel('')
        setSystemPrompt('')
        setPermission(true)
        setMcpServersInput('')
        setSkillIdsInput('')
      }
      setError(null)
    }
  }, [open, editAgent, teams])

  const handleSave = async () => {
    if (!name.trim()) {
      setError(t('teams.agentNameRequired'))
      return
    }
    if (!teamName) {
      setError(t('teams.teamRequired'))
      return
    }

    const mcpServers = mcpServersInput
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const skillIds = skillIdsInput
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)

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
  }

  const teamOptions = teams.map((team) => ({ value: team.name, label: team.name }))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl w-[95vw] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <div className="flex items-center gap-2.5">
            <DialogTitle>{isEdit ? t('teams.editAgent') : t('teams.createAgent')}</DialogTitle>
            {isEdit && <Badge variant="outline">{editAgent?.name}</Badge>}
          </div>
          <DialogDescription>
            {isEdit
              ? t('teams.editAgentDesc')
              : t('teams.createAgentDesc')}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col md:flex-row gap-6 my-2 text-left">
          {/* Left Column: Settings */}
          <div className="flex-1 space-y-4">
            {!isEdit && (
              <Input
                label={t('common.name')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('teams.agentNamePlaceholder')}
              />
            )}

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Select label={t('teams.team')} options={teamOptions} value={teamName} onChange={setTeamName} />
              <Input
                label={t('teams.modelOverride')}
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder={t('teams.modelOverridePlaceholder')}
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label>{t('teams.agentDescription')}</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={2}
                placeholder={t('teams.agentDescPlaceholder')}
              />
            </div>

            {/* Switches in visual cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="flex flex-col gap-2 rounded-lg border border-border p-3 bg-muted/10">
                <div className="flex items-center justify-between">
                  <Label
                    className="text-xs font-semibold cursor-pointer"
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

              <div className="flex flex-col gap-2 rounded-lg border border-border p-3 bg-muted/10">
                <div className="flex items-center justify-between">
                  <Label
                    className="text-xs font-semibold cursor-pointer"
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

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Input
                label={t('teams.mcpServers')}
                value={mcpServersInput}
                onChange={(e) => setMcpServersInput(e.target.value)}
                placeholder={t('teams.mcpServersPlaceholder')}
              />
              <Input
                label={t('teams.skillIds')}
                value={skillIdsInput}
                onChange={(e) => setSkillIdsInput(e.target.value)}
                placeholder={t('teams.skillIdsPlaceholder')}
              />
            </div>
          </div>

          {/* Right Column: System Prompt & Markdown Preview */}
          <div className="flex-1 flex flex-col gap-3 min-h-[350px]">
            <Tabs
              value={promptTab}
              onValueChange={(v: string) => setPromptTab(v as 'edit' | 'preview')}
            >
              <div className="flex items-center justify-between">
                <Label className="font-semibold">{t('teams.systemPrompt')}</Label>
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

              <TabsContent value="edit" className="flex-1 min-h-[300px]">
                <Textarea
                  value={systemPrompt}
                  onChange={(e) => setSystemPrompt(e.target.value)}
                  className="min-h-[300px] font-mono text-xs w-full"
                  placeholder={t('teams.systemPromptPlaceholder')}
                  spellCheck={false}
                />
              </TabsContent>
              <TabsContent
                value="preview"
                className="flex-1 min-h-[300px] max-h-[400px] overflow-y-auto rounded-md border border-border bg-muted/5 p-3 text-sm text-foreground prose prose-sm dark:prose-invert"
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

        {error && <p className="text-xs text-destructive text-left">{error}</p>}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
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
