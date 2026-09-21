import { describe, expect, it } from 'vitest'
import type { ChatMessage } from '@/types'
import { preserveWorkedStateKeys, workedStateKey } from './preserveWorkedStateKeys'

function message(id: string, segments: ChatMessage['segments']): ChatMessage {
  return { id, role: 'assistant', timestamp: '2026-09-18T00:00:00Z', segments }
}
const thinking = { type: 'thinking', text: 'A unique line of reasoning' } as const
const tool = (callId: string) => ({ type: 'tool_call', callId, name: 'read_file', args: '{}', done: false } as const)

describe('preserveWorkedStateKeys', () => {
  it('carries unique exact thinking-only preferences through repeated history hydration', () => {
    const live = message('msg-live', [thinking])
    const first = preserveWorkedStateKeys([live], [message('hist-1', [thinking])])[0]
    const second = preserveWorkedStateKeys([first], [message('hist-1', [thinking])])[0]
    expect(workedStateKey(second, 'worked-0')).toBe(workedStateKey(live, 'worked-0'))
    expect(second.id).toBe('hist-1')
  })

  it('maps each server-split group back to its original worked block', () => {
    const live = message('msg-live', [thinking, tool('a'), tool('b')])
    const history = [message('hist-1', [thinking, tool('a')]), message('hist-2', [tool('b')])]
    const result = preserveWorkedStateKeys([live], history)
    expect(result.map((item) => workedStateKey(item, 'worked-0'))).toEqual([
      workedStateKey(live, 'worked-0'), workedStateKey(live, 'worked-0'),
    ])
  })

  it('preserves one worked preference across content boundaries during history hydration', () => {
    const live = message('msg-live', [tool('a'), { type: 'content', text: 'Progress' }, tool('b')])
    const first = preserveWorkedStateKeys([live], [message('hist-1', [tool('b')])])[0]
    const second = preserveWorkedStateKeys([first], [message('hist-2', [tool('b')])])[0]
    expect(workedStateKey(first, 'worked-0')).toBe(workedStateKey(live, 'worked-2'))
    expect(workedStateKey(second, 'worked-0')).toBe(workedStateKey(live, 'worked-2'))
  })

  it('does not infer identities from repeated reasoning or missing/ambiguous call IDs', () => {
    const incoming = message('hist-1', [thinking])
    expect(preserveWorkedStateKeys([message('a', [thinking]), message('b', [thinking])], [incoming])[0]).toBe(incoming)
    expect(preserveWorkedStateKeys([message('a', [thinking])], [incoming, message('hist-2', [thinking])])[0]).toBe(incoming)
    const emptyCall = message('hist-1', [tool('')])
    expect(preserveWorkedStateKeys([message('a', [tool('')])], [emptyCall])[0]).toBe(emptyCall)
    const ambiguous = message('hist-1', [tool('same')])
    expect(preserveWorkedStateKeys([message('a', [tool('same')]), message('b', [tool('same')])], [ambiguous])[0]).toBe(ambiguous)
  })
})
