import { Fragment } from 'react'
import { Bot, Database, ShieldAlert, CheckCircle2, Loader2 } from 'lucide-react'
import { AgentStateBadge } from './AgentStateBadge'

type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped'

interface AgentInfo {
  id: string
  instance_id: string
  name: string
  state: AgentState
  model_id: string
  provider_id: string
  group: string
  is_leader: boolean
  task_type: string
  last_level?: string
  error_count: number
  last_error: string
  iteration?: number
}

export interface SupervisorInfo {
  group: string
  leader_id: string
  children_ids: string[]
}

interface AgentTableProps {
  agents: AgentInfo[]
  supervisors: SupervisorInfo[] | null
  isConnected: boolean
  onSelectAgent: (instanceId: string) => void
  t: (key: string, v?: Record<string, string | number>) => string
}

function EmptyState({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center px-6">
      <div
        className="w-12 h-12 rounded-full flex items-center justify-center"
        style={{ backgroundColor: 'var(--color-surface-secondary)' }}
      >
        <span style={{ color: 'var(--color-muted-foreground)' }}>{icon}</span>
      </div>
      <span className="text-base font-medium" style={{ color: 'var(--color-foreground)' }}>
        {title}
      </span>
      <span className="text-sm max-w-xs" style={{ color: 'var(--color-muted-foreground)' }}>
        {description}
      </span>
    </div>
  )
}

export function AgentTable({ agents, supervisors, isConnected, onSelectAgent, t }: AgentTableProps) {
  // Group agents by supervisor if available
  const groupedAgents: { key: string; supervisor: string; members: AgentInfo[] }[] = []

  if (supervisors) {
    // Build grouped structure
    const assigned = new Set<string>()
    for (const supervisor of supervisors) {
      const memberIds = [supervisor.leader_id, ...supervisor.children_ids]
      const members = agents.filter(a => memberIds.includes(a.instance_id) || memberIds.includes(a.id))
      members.forEach(m => assigned.add(m.instance_id || m.id))
      if (members.length > 0) {
        groupedAgents.push({ key: supervisor.leader_id, supervisor: supervisor.group, members })
      }
    }
    // Ungrouped agents
    const ungrouped = agents.filter(a => !assigned.has(a.instance_id || a.id))
    if (ungrouped.length > 0) {
      groupedAgents.push({ key: 'ungrouped', supervisor: t('table.groups.ungrouped'), members: ungrouped })
    }
  } else {
    // Flat list — single group
    if (agents.length > 0) {
      groupedAgents.push({ key: 'all', supervisor: t('table.groups.all'), members: agents })
    }
  }

  const renderRow = (agent: AgentInfo, idx: number) => (
    <tr
      key={agent.instance_id}
      className="transition-colors duration-150 cursor-pointer"
      style={{
        borderTop: '1px solid var(--color-border)',
        animationDelay: `${idx * 30}ms`,
      }}
      onClick={() => onSelectAgent(agent.instance_id)}
      onMouseEnter={(e) => {
        e.currentTarget.style.backgroundColor = 'color-mix(in srgb, var(--color-foreground) 4%, transparent)'
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.backgroundColor = 'transparent'
      }}
    >
      <td className="px-4 sm:px-6 py-3">
        <div className="flex items-center gap-2">
          <span className="font-semibold text-sm" style={{ color: 'var(--color-foreground)' }}>
            {agent.name}
          </span>
          {agent.is_leader && (
            <span
              className="px-1.5 py-0.5 rounded text-[10px] font-bold"
              style={{
                backgroundColor: 'color-mix(in srgb, var(--color-warning) 12%, transparent)',
                color: 'var(--color-warning)',
              }}
            >
              {t('table.badges.leader')}
            </span>
          )}
        </div>
      </td>
      <td className="px-4 sm:px-6 py-3">
        <AgentStateBadge state={agent.state} />
      </td>
      <td
        className="px-4 sm:px-6 py-3 text-xs font-mono whitespace-nowrap hidden sm:table-cell"
        style={{ color: 'var(--color-muted-foreground)' }}
      >
        {agent.group || 'Global'}
      </td>
      <td
        className="px-4 sm:px-6 py-3 text-xs font-mono max-w-[160px] truncate whitespace-nowrap"
        title={agent.provider_id ? `${agent.provider_id}/${agent.model_id}` : agent.model_id}
      >
        {agent.provider_id ? (
          <>
            <span style={{ color: 'var(--color-muted-foreground)' }}>{agent.provider_id}/</span>
            <span style={{ color: 'var(--color-foreground)' }}>{agent.model_id}</span>
          </>
        ) : (
          <span style={{ color: 'var(--color-foreground)' }}>{agent.model_id}</span>
        )}
      </td>
      <td className="px-4 sm:px-6 py-3 whitespace-nowrap">
        <span
          className="px-2 py-0.5 rounded text-xs font-mono"
          style={{
            backgroundColor: 'var(--color-surface-secondary)',
            color: 'var(--color-foreground)',
          }}
          title={agent.last_level ? t('table.cols.lastLevel', { level: agent.last_level }) : undefined}
        >
          {agent.task_type}
          {agent.last_level && (
            <span className="ml-1 opacity-50" title={t('table.cols.lastLevel', { level: agent.last_level })}>
              (prev: {agent.last_level})
            </span>
          )}
        </span>
      </td>
      <td className="px-4 sm:px-6 py-3 whitespace-nowrap hidden md:table-cell">
        <span className="text-xs font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
          {agent.iteration ?? 0}
        </span>
      </td>
      <td className="px-4 sm:px-6 py-3 text-right whitespace-nowrap">
        {agent.error_count > 0 ? (
          <span
            className="inline-flex items-center gap-1 text-xs font-semibold"
            style={{ color: 'var(--color-destructive)' }}
            title={agent.last_error || undefined}
          >
            <ShieldAlert className="h-3.5 w-3.5" />
            {agent.error_count}
          </span>
        ) : (
          <span
            className="inline-flex items-center gap-1 text-xs"
            style={{ color: 'var(--color-muted-foreground)' }}
          >
            <CheckCircle2 className="h-3.5 w-3.5" style={{ color: 'var(--color-success)' }} />
            0
          </span>
        )}
      </td>
    </tr>
  )

  return (
    <section
      className="rounded-xl overflow-hidden animate-slide-up shadow-sm"
      style={{ backgroundColor: 'var(--color-card)' }}
    >
      {/* Table header */}
      <div
        className="px-4 sm:px-6 py-4 border-b flex items-center justify-between flex-wrap gap-2"
        style={{ borderColor: 'var(--color-border)' }}
      >
        <h2 className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--color-foreground)' }}>
          <Database className="h-4 w-4" style={{ color: 'var(--color-primary)' }} />
          {t('table.title')}
        </h2>
        <span className="text-xs font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
          {t('table.totalRegistered', { count: isConnected ? agents.length : 0 })}
        </span>
      </div>

      {/* Table body */}
      <div className="table-scroll">
        {!isConnected ? (
          <div className="flex flex-col items-center justify-center py-16 gap-3">
            <Loader2 className="h-6 w-6 animate-spin" style={{ color: 'var(--color-muted-foreground)' }} />
            <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('metrics.connecting')}
            </span>
          </div>
        ) : agents.length === 0 ? (
          <EmptyState
            icon={<Bot className="h-6 w-6" />}
            title={t('table.emptyTitle')}
            description={t('table.emptyDesc')}
          />
        ) : (
          <table className="w-full text-left text-sm border-collapse">
            <thead>
              <tr style={{ backgroundColor: 'var(--color-surface-secondary)' }}>
                <th
                  className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t('table.cols.name')}
                </th>
                <th
                  className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t('table.cols.status')}
                </th>
                <th
                  className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden sm:table-cell"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t('table.cols.group')}
                </th>
                <th
                  className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t('table.cols.model')}
                </th>
                <th
                  className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t('table.cols.taskType')}
                </th>
                <th
                  className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden md:table-cell"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t('table.cols.iteration')}
                </th>
                <th
                  className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap text-right"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t('table.cols.errors')}
                </th>
              </tr>
            </thead>
            <tbody>
              {groupedAgents.map((group) => (
                <Fragment key={group.key}>
                  {/* Group header */}
                  {groupedAgents.length > 1 && (
                    <tr>
                      <td
                        colSpan={7}
                        className="px-4 sm:px-6 py-2 text-[11px] font-semibold uppercase tracking-wider"
                        style={{
                          backgroundColor: 'var(--color-surface-secondary)',
                          color: 'var(--color-accent)',
                        }}
                      >
                        {t('table.groups.supervisor', { name: group.supervisor })}
                      </td>
                    </tr>
                  )}
                  {group.members.map((agent, idx) => renderRow(agent, idx))}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </section>
  )
}
