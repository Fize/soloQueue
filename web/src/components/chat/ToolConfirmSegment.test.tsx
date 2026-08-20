import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ToolConfirmSegment } from './ToolConfirmSegment'
import { StickyToolConfirmPanel } from './StickyToolConfirmPanel'
import { confirmSessionTool } from '@/lib/api'
import { useChatStore } from '@/stores/chatStore'
import { toast } from 'sonner'

vi.mock('@/lib/api', () => ({
  confirmSessionTool: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn() },
}))

describe('ToolConfirmSegment', () => {
  beforeEach(() => {
    useChatStore.setState({
      activeSessionId: 'l2:session-A',
      messages: {},
    })
    vi.clearAllMocks()
  })

  it('shows the backend error and leaves a failed confirmation retryable', async () => {
    const backendError = 'agent: no pending confirmation for call-1'
    vi.mocked(confirmSessionTool).mockRejectedValue(new Error(backendError))
    const segment = {
      type: 'tool_confirm' as const,
      callId: 'call-1',
      name: 'Write',
      prompt: 'Allow write?',
      allowInSession: false,
      resolved: false,
    }

    render(<ToolConfirmSegment segment={segment} />)
    const approve = screen.getByRole('button', { name: /approve/i })
    await userEvent.click(approve)

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(backendError))
    expect(approve).toBeEnabled()
    expect(screen.getByRole('button', { name: /deny/i })).toBeEnabled()
  })

  it('keeps the sticky confirmation unresolved when the backend rejects it', async () => {
    const backendError = 'agent: no pending confirmation for call-sticky'
    vi.mocked(confirmSessionTool).mockRejectedValue(new Error(backendError))
    const segment = {
      type: 'tool_confirm' as const,
      callId: 'call-sticky',
      name: 'Write',
      prompt: 'Allow sticky write?',
      allowInSession: false,
      resolved: false,
    }
    useChatStore.setState({
      messages: {
        'l2:session-A': [
          {
            id: 'assistant-1',
            role: 'assistant',
            timestamp: '',
            segments: [segment],
          },
        ],
      },
    })

    render(<StickyToolConfirmPanel pendingConfirm={segment} />)
    await userEvent.click(screen.getByRole('button', { name: /approve/i }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(backendError))
    expect(useChatStore.getState().messages['l2:session-A'][0].segments[0]).toMatchObject({
      type: 'tool_confirm',
      resolved: false,
    })
  })
})
