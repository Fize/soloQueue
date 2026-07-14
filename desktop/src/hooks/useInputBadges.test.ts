import { describe, it, expect, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useInputBadges } from './useInputBadges'
import type { AgentInfo } from '@/types'

const makeAgent = (overrides: Partial<AgentInfo> = {}): AgentInfo => ({
  id: 'a1',
  instance_id: 'inst-1',
  name: 'Test Agent',
  state: 'processing',
  model_id: 'deepseek-v4-flash',
  provider_id: 'deepseek',
  group: 'L1',
  is_leader: true,
  task_level: '',
  error_count: 0,
  last_error: '',
  pending_delegations: 0,
  mailbox_high: 0,
  mailbox_normal: 0,
  ...overrides,
})

describe('useInputBadges', () => {
  it('returns undefined fields when not processing', () => {
    const agent = makeAgent({ task_level: 'L1-SimpleSingleFile', model_id: 'm' })
    const { result } = renderHook(() => useInputBadges(agent, false))
    expect(result.current.modelName).toBeUndefined()
    expect(result.current.taskLevel).toBeUndefined()
  })

  it('returns the current task_level when set', () => {
    const agent = makeAgent({ task_level: 'L2-MediumMultiFile', model_id: 'm' })
    const { result } = renderHook(() => useInputBadges(agent, true))
    expect(result.current.taskLevel).toBe('L2-MediumMultiFile')
    expect(result.current.modelName).toBe('m')
  })

  it('falls back to last_level when current task_level is empty', () => {
    const agent = makeAgent({
      task_level: '',
      last_level: 'L1-SimpleSingleFile',
      model_id: 'm',
    })
    const { result } = renderHook(() => useInputBadges(agent, true))
    expect(result.current.taskLevel).toBe('L1-SimpleSingleFile')
  })

  it('returns "" sentinel when processing but no level is known yet', () => {
    // First ask: agent is processing, router hasn't classified, no history
    const agent = makeAgent({ task_level: '', last_level: undefined, model_id: 'm' })
    const { result } = renderHook(() => useInputBadges(agent, true))
    expect(result.current.taskLevel).toBe('')
    // Model is gated on the level — even though `model_id` is set on the
    // agent, we don't want to show the template default model alongside
    // an empty level chip. Both stay in placeholder state together.
    expect(result.current.modelName).toBe('')
  })

  it('gates model on level — model stays empty when level is empty', () => {
    // Belt-and-suspenders: confirms the coupling explicitly, regardless
    // of how the test fixture sets the agent's model_id.
    const agent = makeAgent({
      task_level: '',
      last_level: undefined,
      model_id: 'some-template-default',
    })
    const { result } = renderHook(() => useInputBadges(agent, true))
    expect(result.current.taskLevel).toBe('')
    expect(result.current.modelName).toBe('')
  })

  it('does NOT call deriveModel when level is unclassified', () => {
    // deriveModel is the L2 hook that maps level → role → model. If
    // the level isn't classified yet, deriveModel shouldn't run — it
    // would still return the template default and reintroduce the
    // "stale model next to empty level" inconsistency. The hook
    // short-circuits and returns "" for modelName directly.
    const deriveModel = vi.fn(() => 'derived-model')
    const agent = makeAgent({ task_level: '', last_level: undefined, model_id: 'm' })
    const { result } = renderHook(() => useInputBadges(agent, true, deriveModel))
    expect(result.current.taskLevel).toBe('')
    expect(result.current.modelName).toBe('')
    expect(deriveModel).not.toHaveBeenCalled()
  })

  it('keeps the last known level sticky across transient empty values', () => {
    // Render with a real level
    const agentWithLevel = makeAgent({ task_level: 'L0-Conversation', model_id: 'm' })
    const { result, rerender } = renderHook(
      ({ agent }: { agent: AgentInfo }) => useInputBadges(agent, true),
      { initialProps: { agent: agentWithLevel } }
    )
    expect(result.current.taskLevel).toBe('L0-Conversation')
    expect(result.current.modelName).toBe('m')

    // Simulate a transient WebSocket update with empty task_level
    const agentWithoutLevel = makeAgent({ task_level: '', model_id: 'm' })
    rerender({ agent: agentWithoutLevel })
    // Sticky: the level + model keep their resolved values rather than
    // flipping back to the placeholder during a transient gap.
    expect(result.current.taskLevel).toBe('L0-Conversation')
    expect(result.current.modelName).toBe('m')
  })

  it('clears the sticky level when processing stops', () => {
    const agentWithLevel = makeAgent({ task_level: 'L0-Conversation', model_id: 'm' })
    const { result, rerender } = renderHook(
      ({ agent, isProcessing }: { agent: AgentInfo; isProcessing: boolean }) =>
        useInputBadges(agent, isProcessing),
      { initialProps: { agent: agentWithLevel, isProcessing: true } }
    )
    expect(result.current.taskLevel).toBe('L0-Conversation')

    const idleAgent = makeAgent({ state: 'idle', task_level: '', model_id: 'm' })
    rerender({ agent: idleAgent, isProcessing: false })
    expect(result.current.taskLevel).toBeUndefined()
  })

  it('uses deriveModel when provided', () => {
    const agent = makeAgent({ task_level: 'L1-SimpleSingleFile', model_id: '' })
    const deriveModel = (_level: string | undefined, a: AgentInfo | null, _last?: string) =>
      a?.model_id || 'fallback-model'
    const { result } = renderHook(() => useInputBadges(agent, true, deriveModel))
    // model_id is empty, so deriveModel falls back to "fallback-model"
    expect(result.current.modelName).toBe('fallback-model')
  })
})
