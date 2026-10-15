import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { MarkdownPreview } from './markdown-preview'

const streamdownProps = vi.hoisted(() => ({ plugins: {} as Record<string, unknown> }))

vi.mock('streamdown', () => ({
  Streamdown: ({ children, plugins }: { children: string; plugins: Record<string, unknown> }) => {
    streamdownProps.plugins = plugins
    return <div>{children}</div>
  },
}))

describe('MarkdownPreview', () => {
  it('registers the official Mermaid renderer', () => {
    render(<MarkdownPreview content={'```mermaid\nflowchart LR\nA --> B\n```'} />)

    expect(streamdownProps.plugins).toHaveProperty('mermaid')
  })

  it('does not turn untrusted markdown HTML into executable elements', () => {
    const { container } = render(
      <MarkdownPreview
        content={`<script>alert('xss')</script>
<img src="x" onerror="alert('xss')">
<iframe src="https://evil.example"></iframe>
[dangerous](javascript:alert('xss'))`}
      />
    )

    expect(container.querySelector('script')).not.toBeInTheDocument()
    expect(container.querySelector('iframe')).not.toBeInTheDocument()
    expect(container.querySelector('[onerror]')).not.toBeInTheDocument()
    expect(container.querySelector('a[href^="javascript:"]')).not.toBeInTheDocument()
  })
})
