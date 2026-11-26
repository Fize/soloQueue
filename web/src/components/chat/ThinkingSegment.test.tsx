import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ThinkingSegment } from './ThinkingSegment'
import { useChatStore } from '@/stores/chatStore'

vi.mock('@/components/ui/markdown-preview', () => ({
  MarkdownPreview: ({ content }: { content: string }) => <div data-testid="thinking-markdown">{content}</div>,
}))

vi.mock('@/stores/runtimeStore', () => ({
  useRuntimeStore: (selector: (state: { isDesignMode: boolean }) => unknown) =>
    selector({ isDesignMode: false }),
}))

const segment = { type: 'thinking' as const, text: 'weighing the options' }

function toggle(container: HTMLElement) {
  const details = container.querySelector('[data-thinking-batch]') as HTMLDetailsElement
  fireEvent.click(details.querySelector('summary') as HTMLElement)
  return details
}

describe('ThinkingSegment', () => {
  beforeEach(() => {
    useChatStore.setState({ activeSessionId: 'session-a', streamingSessions: {}, activeRequests: {} })
  })

  it('mounts the body only while the batch is open', () => {
    const { container } = render(<ThinkingSegment segment={segment} isLastSegment={false} isStreaming />)
    const details = container.querySelector('[data-thinking-batch]') as HTMLDetailsElement
    expect(details.open).toBe(false)
    expect(screen.queryByTestId('thinking-markdown')).toBeNull()

    toggle(container)
    expect(screen.getByTestId('thinking-markdown')).toHaveTextContent('weighing the options')
  })

  it('opens the batch that is still streaming', () => {
    const { container } = render(<ThinkingSegment segment={segment} isLastSegment isStreaming />)
    expect((container.querySelector('[data-thinking-batch]') as HTMLDetailsElement).open).toBe(true)
    expect(screen.getByTestId('thinking-markdown')).toBeInTheDocument()
  })

  it('keeps the live batch open when the message streams and collapses once it ends', () => {
    const { container, rerender } = render(<ThinkingSegment segment={segment} isLastSegment isStreaming />)
    expect((container.querySelector('[data-thinking-batch]') as HTMLDetailsElement).open).toBe(true)

    rerender(<ThinkingSegment segment={segment} isLastSegment isStreaming={false} />)
    expect((container.querySelector('[data-thinking-batch]') as HTMLDetailsElement).open).toBe(false)
    expect(screen.queryByTestId('thinking-markdown')).toBeNull()
  })

  it('keeps a manual toggle across stream updates', () => {
    const { container, rerender } = render(<ThinkingSegment segment={segment} isLastSegment isStreaming />)
    toggle(container)
    expect(screen.queryByTestId('thinking-markdown')).toBeNull()

    rerender(<ThinkingSegment segment={{ ...segment, text: 'more' }} isLastSegment isStreaming />)
    expect((container.querySelector('[data-thinking-batch]') as HTMLDetailsElement).open).toBe(false)
  })
})
