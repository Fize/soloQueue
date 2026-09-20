import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SegmentView } from './SegmentView'

vi.mock('@/components/DelegationCard', () => ({
  DelegationCard: ({ requestId, agentInstanceId }: { requestId?: string; agentInstanceId?: string }) => (
    <div
      data-testid="delegation-card"
      data-request-id={requestId ?? ''}
      data-agent-instance-id={agentInstanceId ?? ''}
    />
  ),
}))

vi.mock('./WorkedSegment', () => ({
  SubagentCard: ({ requestId }: { requestId?: string }) => (
    <div data-testid="subagent-card" data-request-id={requestId ?? ''} />
  ),
}))

describe('SegmentView request-owned delegation', () => {
  it('passes a concrete request ID to a delegated agent card', () => {
    render(
      <SegmentView
        requestId="request-live"
        segment={{
          type: 'tool_call',
          callId: 'call-1',
          name: 'delegate',
          args: '{"target":"research"}',
          done: false,
        }}
      />,
    )

    expect(screen.getByTestId('delegation-card')).toHaveAttribute('data-request-id', 'request-live')
  })

  it('passes the delegated instance ID for request_team_help cards', () => {
    render(
      <SegmentView
        requestId="request-live"
        segment={{
          type: 'tool_call',
          callId: 'call-help',
          name: 'request_team_help',
          args: '{"team_name":"research"}',
          agentInstanceId: 'peer-instance-1',
          done: false,
        }}
      />,
    )

    expect(screen.getByTestId('delegation-card')).toHaveAttribute(
      'data-agent-instance-id',
      'peer-instance-1',
    )
  })
})
