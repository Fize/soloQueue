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
} from '@/lib/api'
import type { TeamResponse, AgentResponse, CreateTeamRequest } from '@/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Select } from '@/components/ui/select'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Users, Plus, Pencil, Trash2, Loader2 } from 'lucide-react'

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
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isEdit = !!editTeam

  useEffect(() => {
    if (open) {
      if (editTeam) {
        setName(editTeam.name)
        setDescription(editTeam.description || '')
        setWorkspacesJson(
          editTeam.workspaces?.length ? JSON.stringify(editTeam.workspaces, null, 2) : ''
        )
      } else {
        setName('')
        setDescription('')
        setWorkspacesJson('')
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
        })
      } else {
        await createTeam({
          name: name.trim(),
          description: description || undefined,
          workspaces,
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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEdit ? 'Edit Team' : 'Create Team'}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? `Update team "${editTeam?.name}" details`
              : 'Add a new team to organize your agents'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
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
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
              className="flex w-full rounded-md border bg-transparent px-3 py-1.5 text-sm text-foreground transition-colors outline-none placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 resize-y"
              placeholder="Brief description of this team"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label>Workspaces (JSON array)</Label>
            <textarea
              value={workspacesJson}
              onChange={(e) => setWorkspacesJson(e.target.value)}
              rows={4}
              className="flex w-full rounded-md border bg-[#1E1E2E] px-3 py-1.5 font-mono text-xs text-[#E5E7EB] transition-colors outline-none placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 resize-y"
              placeholder='[{"name":"my-project","path":"~/code/my-project"}]'
              spellCheck={false}
            />
          </div>

          {error && <p className="text-xs text-destructive">{error}</p>}
        </div>

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

  const isEdit = !!editAgent

  useEffect(() => {
    if (open) {
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
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEdit ? 'Edit Agent' : 'Create Agent'}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? `Update agent "${editAgent?.name}" configuration`
              : 'Add a new agent to a team'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {!isEdit && (
            <Input
              label="Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Agent name"
            />
          )}

          <Select label="Team" options={teamOptions} value={teamName} onChange={setTeamName} />

          <div className="flex flex-col gap-1.5">
            <Label>Description</Label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
              className="flex w-full rounded-md border bg-transparent px-3 py-1.5 text-sm text-foreground transition-colors outline-none placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 resize-y"
              placeholder="Brief description of this agent"
            />
          </div>

          <Input
            label="Model"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="e.g. deepseek-chat"
          />

          <div className="flex items-center justify-between">
            <Label>Is Leader</Label>
            <Switch checked={isLeader} onCheckedChange={setIsLeader} />
          </div>

          <div className="flex items-center justify-between">
            <Label>Permission (bypass confirm)</Label>
            <Switch checked={permission} onCheckedChange={setPermission} />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label>System Prompt</Label>
            <textarea
              value={systemPrompt}
              onChange={(e) => setSystemPrompt(e.target.value)}
              rows={4}
              className="flex w-full rounded-md border bg-[#1E1E2E] px-3 py-1.5 font-mono text-xs text-[#E5E7EB] transition-colors outline-none placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 resize-y"
              placeholder="System prompt for this agent"
              spellCheck={false}
            />
          </div>

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

          {error && <p className="text-xs text-destructive">{error}</p>}
        </div>

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
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Filter agents by selected team
  const [selectedTeam, setSelectedTeam] = useState<string | null>(null)

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
      const [teamsData, agentsData] = await Promise.all([listTeams(), listAgents()])
      setTeams(teamsData)
      setAgents(agentsData)
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
    if (!window.confirm(`Delete team "${team.name}"? This action cannot be undone.`)) return
    try {
      await deleteTeam(team.name)
      // If the deleted team was selected, clear selection
      if (selectedTeam === team.name) {
        setSelectedTeam(null)
      }
      await fetchData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete team')
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
    if (!window.confirm(`Delete agent "${agent.name}"? This action cannot be undone.`)) return
    try {
      await deleteAgent(agent.name)
      await fetchData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete agent')
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
    </div>
  )
}
