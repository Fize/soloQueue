import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { MarkdownPreview } from './markdown-preview'

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('MarkdownPreview', () => {
  it('renders plain text', () => {
    renderWithRouter(<MarkdownPreview content="Hello" />)
    expect(screen.getByText('Hello')).toBeInTheDocument()
  })

  it('renders bold text', () => {
    renderWithRouter(<MarkdownPreview content="**bold**" />)
    expect(screen.getByText('bold')).toBeInTheDocument()
  })

  it('renders heading', () => {
    renderWithRouter(<MarkdownPreview content="# Heading" />)
    expect(screen.getByText('Heading')).toBeInTheDocument()
  })

  it('renders list', () => {
    renderWithRouter(<MarkdownPreview content="- item" />)
    expect(screen.getByText('item')).toBeInTheDocument()
  })
})
