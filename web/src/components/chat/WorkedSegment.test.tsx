import { render, screen } from '@testing-library/react'
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

describe('WorkedSegment', () => {
  it('renders thinking text through the shared Markdown renderer', () => {
    render(
      <WorkedSegment
        stateKey="worked-markdown"
        group={{
          type: 'worked',
          id: 'worked-0',
          segments: [{
            originalIndex: 0,
            segment: { type: 'thinking', text: '**Inspecting repository history**' },
          }],
          hasToolCalls: false,
          isLast: true,
        }}
      />,
    )

    expect(screen.getByTestId('worked-markdown')).toHaveTextContent(
      '**Inspecting repository history**',
    )
  })
})
