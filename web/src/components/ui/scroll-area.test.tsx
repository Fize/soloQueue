import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ScrollArea } from './scroll-area'

describe('ScrollArea', () => {
  it('renders children', () => {
    render(
      <ScrollArea>
        <p>Scroll content</p>
      </ScrollArea>
    )
    expect(screen.getByText('Scroll content')).toBeInTheDocument()
  })
})
