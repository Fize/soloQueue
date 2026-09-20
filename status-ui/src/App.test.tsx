import { describe, expect, it } from 'vitest'
import { selectAgentStream } from './App'

describe('selectAgentStream', () => {
  it('selects a processing stream from instance/request-keyed runtime state', () => {
    const stream = selectAgentStream(
      {
        'agent-1\u0000old': {
          agent_id: 'agent-1',
          request_id: 'old',
          processing: false,
          segments: [],
          iteration: 1,
        },
        'agent-1\u0000current': {
          agent_id: 'agent-1',
          request_id: 'current',
          processing: true,
          segments: [{ type: 'content', text: 'live' }],
          iteration: 2,
        },
      },
      'agent-1',
    )

    expect(stream?.request_id).toBe('current')
    expect(stream?.segments).toEqual([{ type: 'content', text: 'live' }])
  })

  it('returns the latest available snapshot when the agent is idle', () => {
    const stream = selectAgentStream(
      {
        'agent-1\u0000done': {
          agent_id: 'agent-1',
          request_id: 'done',
          processing: false,
          segments: [{ type: 'content', text: 'done' }],
          iteration: 3,
        },
      },
      'agent-1',
    )

    expect(stream?.request_id).toBe('done')
  })
})
