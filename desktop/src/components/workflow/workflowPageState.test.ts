import { describe, expect, it } from 'vitest'
import { shouldWaitForWorkflowBackend } from './workflowPageState'

describe('shouldWaitForWorkflowBackend', () => {
  it('gates local Electron loads until the backend is healthy', () => {
    expect(shouldWaitForWorkflowBackend(true, 'local', false)).toBe(true)
    expect(shouldWaitForWorkflowBackend(true, 'local', true)).toBe(false)
  })

  it('does not gate browser development or remote connections', () => {
    expect(shouldWaitForWorkflowBackend(false, 'local', false)).toBe(false)
    expect(shouldWaitForWorkflowBackend(true, 'remote', false)).toBe(false)
  })
})
