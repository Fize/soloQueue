import { useState, useEffect } from 'react'
import { useAgentProfile } from '@/hooks/useAgentProfile'
import { useAgentConfig } from '@/hooks/useAgentConfig'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { ScrollArea } from '@/components/ui/scroll-area'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { StatusDot, StatusBadge } from '@/components/ui/status-badge'
import { Badge } from '@/components/ui/badge'
import {
  ArrowLeft,
  Bot,
  Terminal,
  Info,
  Settings,
  Mail,
  AlertTriangle,
  RotateCw,
  FolderOpen,
} from 'lucide-react'
import type { AgentInfo } from '@/types'

interface AgentMonitorPanelProps {
  agents: AgentInfo[]
  groupName: string
  isL1: boolean
}

export function AgentMonitorPanel({ agents, groupName, isL1 }: AgentMonitorPanelProps) {
  const [selectedAgent, setSelectedAgent] = useState<AgentInfo | null>(null)
  const [activeTab, setActiveTab] = useState<'console' | 'status' | 'prompt' | 'config'>('console')

  // Reset selected agent when switching groups/teams
  useEffect(() => {
    setSelectedAgent(null)
  }, [groupName])

  // Keep selected agent in sync with updated list data
  useEffect(() => {
    if (agents.length === 1) {
      setSelectedAgent(agents[0])
    } else if (agents.length === 0) {
      setSelectedAgent(null)
    } else if (selectedAgent) {
      const updated = agents.find(
        (a) =>
          (selectedAgent.instance_id && a.instance_id === selectedAgent.instance_id) ||
          a.id === selectedAgent.id
      )
      if (updated) {
        setSelectedAgent(updated)
      } else {
        setSelectedAgent(null)
      }
    }
  }, [agents, selectedAgent])

  // Get stream for selected agent
  const stream = useAgentStream(selectedAgent?.instance_id ?? null)

  // Get config and profile for selected agent
  const effectiveId = selectedAgent?.id ?? null
  const { profile, loading: profileLoading } = useAgentProfile(isL1 ? 'main' : null)
  const { config, loading: configLoading } = useAgentConfig(
    !isL1 && effectiveId ? effectiveId : null
  )

  const handleBack = () => {
    if (agents.length > 1) {
      setSelectedAgent(null)
    }
  }

  // Render list of agents
  if (!selectedAgent) {
    return (
      <div className="flex flex-col h-full bg-card/20 text-card-foreground">
        <div className="shrink-0 px-4 py-3 border-b border-border/40 bg-card/40 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FolderOpen className="h-4 w-4 text-primary" />
            <h2 className="text-sm font-semibold truncate capitalize">{groupName} Team Monitor</h2>
          </div>
          <Badge variant="outline" className="text-[10px] tabular-nums font-medium">
            {agents.length} Agents
          </Badge>
        </div>

        <ScrollArea className="flex-1 p-3">
          {agents.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground">
              <Bot className="h-8 w-8 text-muted-foreground/30 mb-2" />
              <p className="text-xs font-medium">No active agents in this group</p>
              <p className="text-[10px] text-muted-foreground/60 mt-0.5">
                Agents will appear when a workflow starts.
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {agents.map((agent) => {
                const isRunning = agent.state === 'processing'
                const hasMail = agent.mailbox_high > 0 || agent.mailbox_normal > 0
                return (
                  <button
                    key={agent.instance_id}
                    onClick={() => setSelectedAgent(agent)}
                    className="w-full text-left rounded-xl border border-border/80 bg-card/50 hover:bg-muted/40 p-3.5 transition-all duration-200 cursor-pointer group flex flex-col gap-2.5 relative overflow-hidden"
                  >
                    {isRunning && (
                      <div className="absolute top-0 left-0 right-0 h-[2px] bg-primary animate-pulse" />
                    )}
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2 min-w-0">
                        <StatusDot state={agent.state} />
                        <span className="font-semibold text-xs truncate group-hover:text-primary transition-colors text-foreground">
                          {agent.name}
                        </span>
                      </div>
                      <StatusBadge state={agent.state} size="sm" />
                    </div>

                    <div className="grid grid-cols-2 gap-2 text-[10px] text-muted-foreground">
                      <div className="truncate font-mono bg-muted/30 px-1.5 py-0.5 rounded border border-border/20">
                        {agent.model_id.split('/').pop()}
                      </div>
                      <div className="flex items-center justify-end gap-1 font-mono">
                        {hasMail ? (
                          <span className="flex items-center gap-1 text-primary">
                            <Mail className="h-3 w-3" />
                            {agent.mailbox_high > 0 ? `${agent.mailbox_high}H/` : ''}
                            {agent.mailbox_normal}N
                          </span>
                        ) : (
                          <span className="text-muted-foreground/40">Empty mailbox</span>
                        )}
                      </div>
                    </div>

                    {agent.error_count > 0 && (
                      <div className="flex items-center gap-1.5 text-[10px] text-destructive bg-destructive/5 border border-destructive/20 rounded px-2 py-0.5 mt-0.5">
                        <AlertTriangle className="h-3 w-3 shrink-0" />
                        <span className="truncate">Errors: {agent.error_count}</span>
                      </div>
                    )}
                  </button>
                )
              })}
            </div>
          )}
        </ScrollArea>
      </div>
    )
  }

  // Render detail view of selected agent

  return (
    <div className="flex flex-col h-full bg-card/20 text-card-foreground">
      {/* Header */}
      <div className="shrink-0 px-4 py-3 border-b border-border/40 bg-card/40 flex flex-col gap-2">
        <div className="flex items-center gap-2 min-w-0">
          {agents.length > 1 && (
            <button
              onClick={handleBack}
              className="p-1 rounded hover:bg-muted/80 text-muted-foreground hover:text-foreground transition-colors cursor-pointer mr-1"
              title="Back to agents list"
            >
              <ArrowLeft className="h-4 w-4" />
            </button>
          )}
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 justify-between">
              <span className="font-semibold text-xs truncate text-foreground">
                {selectedAgent.name}
              </span>
              <StatusBadge state={selectedAgent.state} size="sm" />
            </div>
            <p className="font-mono text-[9px] text-muted-foreground/60 truncate mt-0.5">
              {selectedAgent.model_id}
            </p>
          </div>
        </div>

        {/* Tab Controls */}
        <div className="flex border border-border/60 bg-muted/30 rounded-lg p-0.5 gap-0.5 text-[10px] font-semibold mt-1">
          <button
            onClick={() => setActiveTab('console')}
            className={`flex-1 py-1 rounded-md text-center transition-all cursor-pointer ${
              activeTab === 'console'
                ? 'bg-card text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            <span className="flex items-center justify-center gap-1">
              <Terminal className="h-3 w-3" />
              Console
            </span>
          </button>
          <button
            onClick={() => setActiveTab('status')}
            className={`flex-1 py-1 rounded-md text-center transition-all cursor-pointer ${
              activeTab === 'status'
                ? 'bg-card text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            <span className="flex items-center justify-center gap-1">
              <Info className="h-3 w-3" />
              Status
            </span>
          </button>
          <button
            onClick={() => setActiveTab('prompt')}
            className={`flex-1 py-1 rounded-md text-center transition-all cursor-pointer ${
              activeTab === 'prompt'
                ? 'bg-card text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            <span className="flex items-center justify-center gap-1">
              <Settings className="h-3 w-3" />
              Prompt
            </span>
          </button>
        </div>
      </div>

      {/* Tab Contents */}
      <div className="flex-1 overflow-hidden relative">
        {activeTab === 'console' && (
          <ScrollArea className="h-full p-3 bg-card/10">
            {stream ? (
              <AgentStreamView state={stream} />
            ) : (
              <div className="flex flex-col items-center justify-center h-full py-16 text-center text-muted-foreground">
                <Terminal className="h-6 w-6 text-muted-foreground/30 mb-2" />
                <p className="text-xs">No active terminal logs</p>
                <p className="text-[10px] text-muted-foreground/60 mt-0.5">
                  Logs appear when the agent executes tasks.
                </p>
              </div>
            )}
          </ScrollArea>
        )}

        {activeTab === 'status' && (
          <ScrollArea className="h-full p-4 space-y-4">
            {/* Workload Status Card */}
            <div className="bg-card/40 border border-border/60 rounded-xl p-3.5 space-y-3">
              <h3 className="text-xs font-bold text-foreground border-b border-border/20 pb-1.5">
                Workload Details
              </h3>
              <div className="grid grid-cols-2 gap-3 text-xs">
                <div className="space-y-0.5">
                  <span className="text-[9px] text-muted-foreground font-bold uppercase tracking-wider">
                    Delegations
                  </span>
                  <p className="font-bold text-foreground tabular-nums">
                    {selectedAgent.pending_delegations}
                  </p>
                </div>
                <div className="space-y-0.5">
                  <span className="text-[9px] text-muted-foreground font-bold uppercase tracking-wider">
                    Mailbox (H/N)
                  </span>
                  <p className="font-bold text-foreground tabular-nums">
                    {selectedAgent.mailbox_high} / {selectedAgent.mailbox_normal}
                  </p>
                </div>
                <div className="space-y-0.5">
                  <span className="text-[9px] text-muted-foreground font-bold uppercase tracking-wider">
                    Task Level
                  </span>
                  <p className="font-medium text-foreground">
                    Level {selectedAgent.task_level || 'Normal'}
                  </p>
                </div>
                <div className="space-y-0.5">
                  <span className="text-[9px] text-muted-foreground font-bold uppercase tracking-wider">
                    Instance ID
                  </span>
                  <p
                    className="font-mono text-[9px] truncate text-muted-foreground"
                    title={selectedAgent.instance_id}
                  >
                    {selectedAgent.instance_id.slice(0, 8)}...
                  </p>
                </div>
              </div>
            </div>

            {/* Error logs */}
            {selectedAgent.error_count > 0 && (
              <div className="bg-destructive/5 border border-destructive/25 rounded-xl p-3.5 space-y-2">
                <div className="flex items-center gap-1.5 text-destructive">
                  <AlertTriangle className="h-4 w-4 shrink-0" />
                  <h3 className="text-xs font-bold">
                    Errors Detected ({selectedAgent.error_count})
                  </h3>
                </div>
                <ScrollArea className="max-h-40 bg-card/60 rounded border border-border/40 p-2">
                  <pre className="whitespace-pre-wrap font-mono text-[9px] text-destructive leading-relaxed">
                    {selectedAgent.last_error || 'No error details recorded'}
                  </pre>
                </ScrollArea>
              </div>
            )}
          </ScrollArea>
        )}

        {activeTab === 'prompt' && (
          <ScrollArea className="h-full p-4">
            <div className="bg-card/40 border border-border/60 rounded-xl p-3 space-y-2">
              <div className="flex items-center justify-between border-b border-border/20 pb-1.5 px-1">
                <span className="text-[9px] text-muted-foreground uppercase font-bold tracking-wider">
                  {isL1 ? 'System Soul Prompt' : 'Agent Prompt'}
                </span>
              </div>
              <div className="bg-card border border-border/40 rounded p-2.5 max-h-[50vh] overflow-y-auto">
                {profileLoading || configLoading ? (
                  <div className="flex justify-center py-6 text-muted-foreground text-xs">
                    <RotateCw className="h-4 w-4 animate-spin mr-1.5" />
                    Loading prompt...
                  </div>
                ) : isL1 && profile?.soul ? (
                  <MarkdownPreview content={profile.soul} />
                ) : !isL1 && config?.system_prompt ? (
                  <MarkdownPreview content={config.system_prompt} />
                ) : (
                  <p className="text-[10px] text-muted-foreground italic text-center py-4">
                    No prompt configured
                  </p>
                )}
              </div>
            </div>
          </ScrollArea>
        )}
      </div>
    </div>
  )
}
