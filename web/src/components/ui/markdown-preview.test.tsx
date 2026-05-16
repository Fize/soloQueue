import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MarkdownPreview } from './markdown-preview'

describe('MarkdownPreview', () => {
  it('renders plain text', () => {
    render(<MarkdownPreview content="Hello" />)
    expect(screen.getByText('Hello')).toBeInTheDocument()
  })

  it('renders bold text', () => {
    render(<MarkdownPreview content="**bold**" />)
    expect(screen.getByText('bold')).toBeInTheDocument()
  })

  it('renders heading', () => {
    render(<MarkdownPreview content="# Heading" />)
    expect(screen.getByText('Heading')).toBeInTheDocument()
  })

  it('renders list', () => {
    render(<MarkdownPreview content="- item" />)
    expect(screen.getByText('item')).toBeInTheDocument()
  })
})
