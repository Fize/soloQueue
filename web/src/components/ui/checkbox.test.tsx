import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Checkbox } from './checkbox'

describe('Checkbox', () => {
  it('renders unchecked', () => {
    render(<Checkbox />)
    const cb = screen.getByRole('checkbox')
    expect(cb).toBeInTheDocument()
    expect(cb).not.toBeChecked()
  })

  it('renders checked', () => {
    render(<Checkbox checked />)
    expect(screen.getByRole('checkbox')).toBeChecked()
  })
})
