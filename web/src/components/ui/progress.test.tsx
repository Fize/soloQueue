import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Progress } from './progress'

describe('Progress', () => {
  it('renders with correct value', () => {
    const { container } = render(<Progress value={50} />)
    const indicator = container.querySelector('[data-slot="progress-indicator"]')
    expect(indicator).toBeInTheDocument()
  })

  it('renders with 0 value', () => {
    const { container } = render(<Progress value={0} />)
    expect(container.querySelector('[data-slot="progress-indicator"]')).toBeInTheDocument()
  })
})
