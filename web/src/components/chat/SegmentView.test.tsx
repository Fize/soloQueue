import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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

describe('SegmentView compact event row', () => {
  it('collapses the summary behind one 28px event row', async () => {
    const user = userEvent.setup()
    render(<SegmentView segment={{ type: 'compact', text: '## Summary\n\nkept paragraph' }} />)

    const row = screen.getByRole('button', { name: 'Show compacted summary' })
    expect(row).toHaveAttribute('aria-expanded', 'false')
    expect(row).toHaveTextContent('Context compacted')
    expect(screen.queryByText('kept paragraph')).not.toBeInTheDocument()

    await user.click(row)
    expect(row).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByText('kept paragraph')).toBeInTheDocument()

    await user.click(row)
    expect(row).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('kept paragraph')).not.toBeInTheDocument()
  })

  it('copies the raw markdown from the expanded block', async () => {
    const user = userEvent.setup()
    render(<SegmentView segment={{ type: 'compact', text: '## Summary\n\nkept paragraph' }} />)

    await user.click(screen.getByRole('button', { name: 'Show compacted summary' }))
    await user.click(screen.getByRole('button', { name: 'Copy summary' }))

    await expect(navigator.clipboard.readText()).resolves.toBe('## Summary\n\nkept paragraph')
  })

  it('renders a row without a disclosure for an empty summary', () => {
    render(<SegmentView segment={{ type: 'compact', text: '   ' }} />)

    expect(screen.getByText('Context compacted')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
