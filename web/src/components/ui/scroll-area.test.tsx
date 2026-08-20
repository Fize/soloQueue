import { createRef } from 'react'
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

  it('exposes the scroll viewport through viewportRef', () => {
    const viewportRef = createRef<HTMLDivElement>()

    render(
      <ScrollArea viewportRef={viewportRef}>
        <p>Scroll content</p>
      </ScrollArea>
    )

    expect(viewportRef.current).toHaveAttribute('data-slot', 'scroll-area-viewport')
  })
})
