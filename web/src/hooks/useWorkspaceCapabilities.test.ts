import { act, render, renderHook, screen } from '@testing-library/react'
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { createElement } from 'react'
import { classifyWorkspace, useWorkspaceCapabilities } from './useWorkspaceCapabilities'
import { ChatInput } from '@/components/ChatInput'

vi.mock('@/lib/api', () => ({
  getProjectBranches: vi.fn().mockResolvedValue(['main']),
  uploadFile: vi.fn(),
}))

describe('classifyWorkspace', () => {
  it.each([
    [719, 700, 'phone', false, 'single'],
    [720, 599, 'phone', false, 'single'],
    [720, 600, 'pad', true, 'single'],
    [999, 700, 'pad', true, 'single'],
    [1000, 700, 'pad', true, 'split'],
    [1199, 700, 'pad', true, 'split'],
    [1200, 699, 'phone', false, 'single'],
    [1200, 700, 'desktop', true, 'split'],
  ] as const)('classifies %dx%d as %s', (width, height, workspace, canUseDesignMode, designLayout) => {
    expect(classifyWorkspace(width, height)).toMatchObject({ workspace, canUseDesignMode, designLayout })
  })
})

describe('useWorkspaceCapabilities', () => {
  const originalInnerWidth = window.innerWidth
  const originalInnerHeight = window.innerHeight

  beforeEach(() => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 720 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 600 })
  })

  afterEach(() => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: originalInnerWidth })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: originalInnerHeight })
  })

  it('updates when the effective viewport changes', () => {
    const { result } = renderHook(() => useWorkspaceCapabilities())
    expect(result.current.workspace).toBe('pad')
    expect(result.current.designLayout).toBe('single')

    act(() => {
      Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1000 })
      window.dispatchEvent(new Event('resize'))
    })

    expect(result.current.workspace).toBe('pad')
    expect(result.current.designLayout).toBe('split')
  })

  it('hides the Design Mode toggle in Phone capability', () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 719 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 800 })

    render(
      createElement(ChatInput, {
        onSend: vi.fn(),
        onCancel: vi.fn(),
        streaming: false,
        delegating: false,
        disabled: false,
        activeSessionId: 'l2-session',
      }),
    )

    expect(screen.queryByTitle('Design Mode')).not.toBeInTheDocument()
  })

  it('keeps the Design Mode toggle unavailable for L1 on a capable workspace', () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1000 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 800 })

    render(
      createElement(ChatInput, {
        onSend: vi.fn(),
        onCancel: vi.fn(),
        streaming: false,
        delegating: false,
        disabled: false,
        activeSessionId: 'l1',
      }),
    )

    expect(screen.queryByTitle('Design Mode')).not.toBeInTheDocument()
  })
})
