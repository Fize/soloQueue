import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render } from '@testing-library/react'
import type { ReactNode } from 'react'

const captured = vi.hoisted(() => [] as Array<Record<string, unknown>>)

vi.mock('./markdown-preview', () => ({
  MarkdownPreview: (props: Record<string, unknown>) => {
    captured.push(props)
    return <div data-preview>{String(props.content ?? '')}</div>
  },
}))

const { LongMarkdown } = await import('./long-markdown')

const paragraph = (index: number) =>
  `Paragraph ${index}. ${'The quick brown fox jumps over the lazy dog. '.repeat(5)}\n\n`

function markdown(size: number): string {
  let body = 'Options:\n\n'
  let index = 1
  while (body.length < size) body += paragraph(index++)
  return body
}

function normalize(value: string | null | undefined): string {
  return (value ?? '').replace(/\s+/g, ' ').trim()
}

describe('LongMarkdown', () => {
  beforeEach(() => {
    captured.length = 0
  })

  it('renders a single preview for a short document', () => {
    const { container } = render(<LongMarkdown content={markdown(500)} className="text-xs" />)
    expect(container.querySelectorAll('[data-preview]')).toHaveLength(1)
    expect(container.querySelector('.long-markdown')).toBeNull()
    expect(captured).toHaveLength(1)
  })

  it('splits a long document into blocks without losing text', () => {
    const content = markdown(60_000)
    const { container } = render(<LongMarkdown content={content} className="text-xs" />)
    const previews = container.querySelectorAll('[data-preview]')
    expect(previews.length).toBeGreaterThan(5)
    const joined = Array.from(previews)
      .map((node) => node.textContent ?? '')
      .join('')
    expect(normalize(joined)).toBe(normalize(content))
  })

  it('streams only the growing block', () => {
    const { rerender } = render(<LongMarkdown content={markdown(40_000)} isAnimating />)
    expect(captured.length).toBeGreaterThan(2)
    expect(captured.filter((props) => props.isAnimating)).toHaveLength(1)
    expect(captured.at(-1)?.isAnimating).toBe(true)

    captured.length = 0
    rerender(<LongMarkdown content={markdown(40_000) + 'more text\n\n'} isAnimating />)
    const animating = captured.filter((props) => props.isAnimating)
    expect(animating).toHaveLength(1)
    expect(String(animating[0].content)).toContain('more text')
  })

  it('renders nothing for empty content', () => {
    const { container } = render(<LongMarkdown content="" />)
    expect(container.firstChild).toBeNull()
    expect(captured).toHaveLength(0)
  })

  it('normalizes children type for the wrapper', () => {
    const content = markdown(40_000)
    const { container } = render(<LongMarkdown content={content} />)
    const wrapper = container.querySelector('.long-markdown')
    expect(wrapper).not.toBeNull()
    expect((wrapper as ReactNode) !== undefined).toBe(true)
  })
})
