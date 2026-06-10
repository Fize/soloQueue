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
      setError('Team name is required')
      return
    }

    // Parse workspaces JSON if provided
    let workspaces: CreateTeamRequest['workspaces'] = undefined
    if (workspacesJson.trim()) {
      try {
        workspaces = JSON.parse(workspacesJson)
        if (!Array.isArray(workspaces)) {
          setError('Workspaces must be a JSON array')
          return
        }
      } catch {
        setError('Invalid JSON in workspaces')
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
      setError(err instanceof Error ? err.message : 'Failed to save team')
    } finally {
      setSaving(false)
    }
  }

  // Generate workspaces Markdown preview
  let workspacesPreviewMD = '### Workspaces Configured\n\n'
  if (workspacesJson.trim()) {
    try {
      const parsed = JSON.parse(workspacesJson)
      if (Array.isArray(parsed) && parsed.length > 0) {
        parsed.forEach((ws: any, idx: number) => {
          const wsName = ws.name || `Workspace #${idx + 1}`
          const wsPath = ws.path || '*No path set*'
          workspacesPreviewMD += `- **${wsName}**: \`${wsPath}\`\n`
          if (ws.autoWork?.enabled) {
            workspacesPreviewMD += `  - *AutoWork*: Cooldown: ${ws.autoWork.initialCooldownMinutes}m / Max: ${ws.autoWork.maxIntervalsPerDay} intervals/day\n`
          }
        })
      } else {
        workspacesPreviewMD += '*No workspaces configured.*'
      }
    } catch {
      workspacesPreviewMD =
        '⚠️ **Invalid JSON Format**\n\nPlease switch to **Edit** mode to correct the JSON syntax.'
    }
  } else {
    workspacesPreviewMD += '*No workspaces configured yet.*'
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="md:max-w-4xl w-[95vw] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <DialogTitle>{isEdit ? 'Edit Team' : 'Create Team'}</DialogTitle>
            {isEdit && <Badge variant="outline">{editTeam?.name}</Badge>}
          </div>
          <DialogDescription>
            {isEdit
              ? `Update team "${editTeam?.name}" details`
              : 'Add a new team to organize your agents'}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col md:flex-row gap-6 my-2 text-left">
          {/* Left Column: Info */}
          <div className="flex-1 space-y-4">
            {!isEdit && (
              <Input
                label="Name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Team name"
              />
            )}

            <div className="flex flex-col gap-1.5">
              <Label>Description</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={4}
                placeholder="Brief description of this team"
              />
            </div>

            <div className="flex flex-col gap-1.5 pt-2">
              <Label className="font-semibold">Associated Projects</Label>
              <div className="border border-border rounded-md p-3 max-h-[180px] overflow-y-auto space-y-2 bg-muted/10">
                {allProjects.length === 0 ? (
                  <p className="text-xs text-muted-foreground italic">
                    No projects created yet. Create projects in the Projects tab.
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
                <Label className="font-semibold">Workspaces Configuration</Label>
                <TabsList className="bg-muted/60 p-0.5 rounded-md border border-border">
                  <TabsTrigger
                    value="edit"
                    className="flex items-center gap-1 rounded-[4px] px-2.5 py-1 text-xs font-medium"
                  >
                    <FileTextIcon className="h-3 w-3" />
                    Edit JSON
                  </TabsTrigger>
                  <TabsTrigger
                    value="preview"
                    className="flex items-center gap-1 rounded-[4px] px-2.5 py-1 text-xs font-medium"
                  >
                    <Eye className="h-3 w-3" />
                    Preview
                  </TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="edit" className="flex-1 flex flex-col min-h-[220px]">
                <div className="flex flex-col gap-1.5 h-full">
                  <Textarea
                    value={workspacesJson}
                    onChange={(e) => setWorkspacesJson(e.target.value)}
                    className="flex-1 min-h-[220px] font-mono text-xs"
                    placeholder='[{"name":"my-project","path":"~/code/my-project"}]'
                    spellCheck={false}
                  />
                  <p className="text-[10px] text-muted-foreground/80 leading-normal">
                    Enter valid JSON array representing project workspace configurations.
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
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                Saving...
              </>
            ) : isEdit ? (
              'Save Changes'
            ) : (
              'Create Team'
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
      setError('Agent name is required')
      return
    }
    if (!teamName) {
      setError('Team is required')
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
      setError(err instanceof Error ? err.message : 'Failed to save agent')
    } finally {
      setSaving(false)
    }
  }

  const teamOptions = teams.map((t) => ({ value: t.name, label: t.name }))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="md:max-w-4xl w-[95vw] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <DialogTitle>{isEdit ? 'Edit Agent' : 'Create Agent'}</DialogTitle>
            {isEdit && <Badge variant="outline">{editAgent?.name}</Badge>}
          </div>
          <DialogDescription>
            {isEdit
              ? `Configure settings and prompt rules for this agent`
              : 'Configure and register a new team member'}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col md:flex-row gap-6 my-2 text-left">
          {/* Left Column: Settings */}
          <div className="flex-1 space-y-4">
            {!isEdit && (
              <Input
                label="Name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Agent name"
              />
            )}

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Select label="Team" options={teamOptions} value={teamName} onChange={setTeamName} />
              <Input
                label="Model Override"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="e.g. deepseek-chat"
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label>Description</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={2}
                placeholder="Brief description of this agent's capabilities"
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
                    Is Leader
                  </Label>
                  <Switch id="is-leader-switch" checked={isLeader} onCheckedChange={setIsLeader} />
                </div>
                <p className="text-[10px] text-muted-foreground leading-normal">
                  Orchestrates tasks, plans, and delegates to worker sub-agents.
                </p>
              </div>

              <div className="flex flex-col gap-2 rounded-lg border border-border p-3 bg-muted/10">
                <div className="flex items-center justify-between">
                  <Label
                    className="text-xs font-semibold cursor-pointer"
                    htmlFor="permission-switch"
                  >
                    Bypass Confirm
                  </Label>
                  <Switch
                    id="permission-switch"
                    checked={permission}
                    onCheckedChange={setPermission}
                  />
                </div>
                <p className="text-[10px] text-muted-foreground leading-normal">
                  Skip confirmation dialogs when running tools (automatic execution).
                </p>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Input
                label="MCP Servers (comma-separated)"
                value={mcpServersInput}
                onChange={(e) => setMcpServersInput(e.target.value)}
                placeholder="server1, server2"
              />
              <Input
                label="Skill IDs (comma-separated)"
                value={skillIdsInput}
                onChange={(e) => setSkillIdsInput(e.target.value)}
                placeholder="skill-id-1, skill-id-2"
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
                <Label className="font-semibold">System Prompt</Label>
                <TabsList className="bg-muted/60 p-0.5 rounded-md border border-border">
                  <TabsTrigger
                    value="edit"
                    className="flex items-center gap-1 rounded-[4px] px-2.5 py-1 text-xs font-medium"
                  >
                    <FileTextIcon className="h-3 w-3" />
                    Edit
                  </TabsTrigger>
                  <TabsTrigger
                    value="preview"
                    className="flex items-center gap-1 rounded-[4px] px-2.5 py-1 text-xs font-medium"
                  >
                    <Eye className="h-3 w-3" />
                    Preview
                  </TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="edit" className="flex-1 min-h-[300px]">
                <Textarea
                  value={systemPrompt}
                  onChange={(e) => setSystemPrompt(e.target.value)}
                  className="min-h-[300px] font-mono text-xs w-full"
                  placeholder="Paste or write the system instructions here..."
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
                    No prompt instructions entered yet.
                  </span>
                )}
              </TabsContent>
            </Tabs>
          </div>
        </div>

        {error && <p className="text-xs text-destructive text-left">{error}</p>}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                Saving...
              </>
            ) : isEdit ? (
              'Save Changes'
            ) : (
              'Create Agent'
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
  const [error, setError] = useState<string | null>(null)

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
    setError(null)
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
      setError(err instanceof Error ? err.message : 'Failed to load data')
    } finally {
      setLoading(false)
    }
  }, [])

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
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete team')
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
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete agent')
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
    return <div className="text-sm text-muted-foreground">Loading agents &amp; teams...</div>
  }

  return (
    <div className="space-y-6">
      {/* Page-level error */}
      {error && (
        <div className="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-2 text-xs text-destructive">
          {error}
        </div>
      )}

      {/* ── Teams Section ──────────────────────────────────────────────── */}
      <div className="border rounded-lg bg-card shadow-sm">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div className="flex items-center gap-2">
            <Users className="h-4 w-4 text-foreground" />
            <h3 className="text-sm font-bold text-foreground">Teams</h3>
            <Badge variant="secondary" className="text-[10px]">
              {teams.length}
            </Badge>
          </div>
          <Button size="sm" onClick={handleCreateTeam} className="gap-1">
            <Plus className="h-3.5 w-3.5" />
            Create Team
          </Button>
        </div>

        {teams.length === 0 ? (
          <div className="px-5 py-6 text-center">
            <p className="text-sm text-muted-foreground">No teams created yet</p>
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
                        {count} {count === 1 ? 'agent' : 'agents'}
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
                      title="Edit team"
                    >
                      <Pencil className="h-3 w-3" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      onClick={() => handleDeleteTeam(team)}
                      title="Delete team"
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
            <h3 className="text-sm font-bold text-foreground">Agents</h3>
            <Badge variant="secondary" className="text-[10px]">
              {filteredAgents.length}
            </Badge>
            {selectedTeam && (
              <Badge
                variant="primary"
                className="text-[10px] cursor-pointer"
                onClick={() => setSelectedTeam(null)}
              >
                Team: {selectedTeam} ✕
              </Badge>
            )}
          </div>
          <Button size="sm" onClick={handleCreateAgent} className="gap-1">
            <Plus className="h-3.5 w-3.5" />
            Create Agent
          </Button>
        </div>

        {filteredAgents.length === 0 ? (
          <div className="px-5 py-6 text-center">
            <p className="text-sm text-muted-foreground">
              {selectedTeam ? 'No agents in this team' : 'No agents created yet'}
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
                      Team: {agent.team_name}
                    </Badge>
                    {agent.is_leader && (
                      <Badge variant="primary" className="text-[10px]">
                        Leader
                      </Badge>
                    )}
                    {agent.permission && (
                      <Badge variant="success" className="text-[10px]">
                        Bypass
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
                      Model: {agent.model}
                    </p>
                  )}
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => handleEditAgent(agent)}
                    title="Edit agent"
                  >
                    <Pencil className="h-3 w-3" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => handleDeleteAgent(agent)}
                    title="Delete agent"
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
        title="Delete Team"
        message={`Delete team "${deleteTeamTarget?.name}"? This action cannot be undone.`}
        destructive
        onConfirm={confirmDeleteTeam}
        confirmLabel="Delete Team"
      />
      <ConfirmDialog
        open={!!deleteAgentTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteAgentTarget(null)
        }}
        title="Delete Agent"
        message={`Delete agent "${deleteAgentTarget?.name}"? This action cannot be undone.`}
        destructive
        onConfirm={confirmDeleteAgent}
        confirmLabel="Delete Agent"
      />
    </div>
  )
}
