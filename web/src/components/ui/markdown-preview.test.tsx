import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { MarkdownPreview } from './markdown-preview'

describe('MarkdownPreview', () => {
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
