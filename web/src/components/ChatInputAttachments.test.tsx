import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ChatInputAttachments, type Attachment } from './ChatInputAttachments'

function makeAttachment(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: '1',
    file: new File([], 'test.png'),
    name: 'test.png',
    previewUrl: 'blob:test',
    status: 'done',
    ...overrides,
  }
}

describe('ChatInputAttachments', () => {
  it('renders nothing when no attachments and no selectedTarget', () => {
    const { container } = render(
      <ChatInputAttachments attachments={[]} onRemove={() => {}} />
    )
    expect(container.innerHTML).toBe('')
  })

  it('renders attachment thumbnail', () => {
    render(
      <ChatInputAttachments
        attachments={[makeAttachment()]}
        onRemove={() => {}}
      />
    )
    const img = screen.getByAltText('preview')
    expect(img).toBeInTheDocument()
    expect(img).toHaveAttribute('src', 'blob:test')
  })

  it('shows uploading overlay when status is uploading', () => {
    render(
      <ChatInputAttachments
        attachments={[makeAttachment({ status: 'uploading' })]}
        onRemove={() => {}}
      />
    )
    // The Loader2 spinner should be visible
    const img = screen.getByAltText('preview')
    const parent = img.parentElement!
    // The overlay div contains a Loader2 icon (Lucide renders an svg)
    const loader = parent.querySelector('.animate-spin')
    expect(loader).toBeInTheDocument()
  })

  it('shows failed overlay when status is failed', () => {
    render(
      <ChatInputAttachments
        attachments={[makeAttachment({ status: 'failed', error: 'Upload timeout' })]}
        onRemove={() => {}}
      />
    )
    expect(screen.getByText('Failed')).toBeInTheDocument()
    // The error message should be on the title attribute
    const failedEl = screen.getByText('Failed').parentElement
    expect(failedEl).toHaveAttribute('title', 'Upload timeout')
  })

  it('renders selected DOM target badge', () => {
    render(
      <ChatInputAttachments
        attachments={[]}
        selectedTarget={{
          filePath: '/foo.html',
          selector: '#main',
          text: 'Hello World',
          htmlHint: '<div id="main">Hello World</div>',
        }}
        onClearSelectedTarget={() => {}}
        onRemove={() => {}}
      />
    )
    // Check for the code element with the selector
    expect(screen.getByText('#main')).toBeInTheDocument()
    // The text is in a span with truncation
    expect(screen.getByText(/Hello World/)).toBeInTheDocument()
    // A button with "Deselect element" title should exist
    expect(screen.getByTitle('Deselect element')).toBeInTheDocument()
  })

  it('calls onRemove when remove button is clicked', () => {
    const onRemove = vi.fn()
    render(
      <ChatInputAttachments
        attachments={[makeAttachment()]}
        onRemove={onRemove}
      />
    )
    // The remove X button — find by title since it's always rendered but invisible until hover
    const removeBtn = screen.getByTitle('Remove image')
    removeBtn.click()
    expect(onRemove).toHaveBeenCalledWith('1')
  })
})
