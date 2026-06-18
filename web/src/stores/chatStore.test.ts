import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useChatStore } from './chatStore'

beforeEach(() => {
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    messages: {},
    streaming: false,
    delegating: false,
    titleGenerated: {},
    historyLoading: {},
    historyHasMore: {},
    pendingHistory: {},
  })
  vi.clearAllMocks()
})

describe('chatStore', () => {
  it('updateToolCallResult updates tool call segment done state in any assistant message', () => {
    const sid = 'session-1'
    useChatStore.setState({
      activeSessionId: sid,
      messages: {
        [sid]: [
          {
            id: 'msg-1',
            role: 'assistant',
            timestamp: '',
            segments: [
              {
                type: 'tool_call',
                callId: 'call-1',
                name: 'test_tool',
                args: '{}',
                done: false,
              },
            ],
          },
          {
            id: 'msg-2',
            role: 'user',
            timestamp: '',
            segments: [{ type: 'content', text: 'user msg' }],
          },
          {
            id: 'msg-3',
            role: 'assistant',
            timestamp: '',
            segments: [
              {
                type: 'tool_call',
                callId: 'call-2',
                name: 'test_tool_2',
                args: '{}',
                done: false,
              },
            ],
          },
        ],
      },
    })

    // Update first tool call (which is not in the last message)
    useChatStore.getState().updateToolCallResult('call-1', 'result-1', undefined, 100)

    const msgs = useChatStore.getState().messages[sid]
    expect(msgs).toBeDefined()
    expect(msgs[0].segments[0]).toEqual({
      type: 'tool_call',
      callId: 'call-1',
      name: 'test_tool',
      args: '{}',
      result: 'result-1',
      error: undefined,
      durationMs: 100,
      done: true,
    })

    // Update second tool call (which is in the last message)
    useChatStore.getState().updateToolCallResult('call-2', 'result-2', 'error-2', 200)
    const updatedMsgs = useChatStore.getState().messages[sid]
    expect(updatedMsgs[2].segments[0]).toEqual({
      type: 'tool_call',
      callId: 'call-2',
      name: 'test_tool_2',
      args: '{}',
      result: 'result-2',
      error: 'error-2',
      durationMs: 200,
      done: true,
    })
  })
})
