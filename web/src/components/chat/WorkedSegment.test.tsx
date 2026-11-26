import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { WorkedSegment } from './WorkedSegment'

vi.mock('@/components/ui/markdown-preview', () => ({
  MarkdownPreview: ({ content }: { content: string }) => (
    <div data-testid="worked-markdown">{content}</div>
  ),
}))

vi.mock('@/stores/runtimeStore', () => ({
  useRuntimeStore: (selector: (state: { isDesignMode: boolean }) => unknown) =>
    selector({ isDesignMode: false }),
}))

/** The batch summary is labelled by i18n; click the element itself. */
function toggle(details: HTMLDetailsElement) {
  fireEvent.click(details.querySelector('summary') as HTMLElement)
}

function groupWith(segments: Array<{ type: 'thinking'; text: string }>, isLast = true) {
  return {
    type: 'worked' as const,
    id: 'worked-0',
    segments: segments.map((segment, index) => ({ segment, originalIndex: index })),
    hasToolCalls: false,
    isLast,
  }
}

describe('WorkedSegment', () => {
  it('renders the streaming thinking batch through the shared Markdown renderer', () => {
    render(<WorkedSegment stateKey="worked-markdown" group={groupWith([{ type: 'thinking', text: '**Inspecting repository history**' }])} isStreaming />)

    const batch = document.querySelector('[data-thinking-batch]') as HTMLDetailsElement
    expect(batch).not.toBeNull()
    expect(batch.open).toBe(true)
    expect(screen.getByTestId('worked-markdown')).toHaveTextContent('**Inspecting repository history**')
  })

  it('collapses a finished batch and mounts its content only when opened', () => {
    const { container } = render(
      <WorkedSegment
        stateKey="worked-finished"
        group={groupWith([
          { type: 'thinking', text: 'first batch' },
          { type: 'thinking', text: 'second batch' },
        ])}
        isStreaming
      />,
    )

    const batches = container.querySelectorAll('[data-thinking-batch]')
    expect(batches).toHaveLength(2)
    // Only the batch that is still streaming is open, and only it is mounted.
    expect((batches[0] as HTMLDetailsElement).open).toBe(false)
    expect((batches[1] as HTMLDetailsElement).open).toBe(true)
    expect(screen.getAllByTestId('worked-markdown')).toHaveLength(1)
    expect(within(batches[1] as HTMLElement).getByTestId('worked-markdown')).toHaveTextContent('second batch')

    toggle(batches[0] as HTMLDetailsElement)
    expect((batches[0] as HTMLDetailsElement).open).toBe(true)
    expect(within(batches[0] as HTMLElement).getByTestId('worked-markdown')).toHaveTextContent('first batch')
  })

  it('keeps every batch inside the worked group and mounts nothing while it is collapsed', () => {
    const { container } = render(
      <WorkedSegment stateKey="worked-body" group={groupWith([{ type: 'thinking', text: 'hidden' }])} isStreaming={false} />,
    )

    const worked = container.querySelector('[data-worked]') as HTMLDetailsElement
    expect(worked.open).toBe(false)
    expect(worked.querySelector('[data-thinking-batch]')).toBeNull()

    toggle(worked)
    expect((container.querySelector('[data-worked]') as HTMLDetailsElement).open).toBe(true)
    const batch = container.querySelector('[data-thinking-batch]') as HTMLDetailsElement
    expect(batch.closest('[data-worked]')).toBe(container.querySelector('[data-worked]'))
  })
})
