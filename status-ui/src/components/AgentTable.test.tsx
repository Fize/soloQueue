import { renderToStaticMarkup } from 'react-dom/server'
import { expect, it } from 'vitest'
import { AgentTable, type SupervisorInfo } from './AgentTable'

const agents = [
  {
    id: 'leader-template',
    instance_id: 'leader-instance',
    name: 'Leader Agent',
    state: 'idle' as const,
    model_id: 'model',
    provider_id: 'provider',
    group: 'research-team',
    is_leader: true,
    task_type: 'engineering',
    error_count: 0,
    last_error: '',
  },
  {
    id: 'child-template',
    instance_id: 'child-instance',
    name: 'Child Agent',
    state: 'processing' as const,
    model_id: 'model',
    provider_id: 'provider',
    group: 'research-team',
    is_leader: false,
    task_type: 'engineering',
    error_count: 0,
    last_error: '',
  },
  {
    id: 'ungrouped-template',
    instance_id: 'ungrouped-instance',
    name: 'Ungrouped Agent',
    state: 'idle' as const,
    model_id: 'model',
    provider_id: 'provider',
    group: '',
    is_leader: false,
    task_type: 'general',
    error_count: 0,
    last_error: '',
  },
]

const supervisors: SupervisorInfo[] = [
  {
    group: 'research-team',
    leader_id: 'leader-instance',
    children_ids: ['child-instance'],
  },
]

it('renders leader, child, and ungrouped agents from backend-shaped supervisors', () => {
  const html = renderToStaticMarkup(
    <AgentTable
      agents={agents}
      supervisors={supervisors}
      isConnected
      onSelectAgent={() => {}}
      t={(key, values) => (values?.name ? `${key}:${values.name}` : key)}
    />,
  )

  expect(html).toContain('Leader Agent')
  expect(html).toContain('Child Agent')
  expect(html).toContain('Ungrouped Agent')
})
