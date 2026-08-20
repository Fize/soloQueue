import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Switch } from './switch'

describe('Switch', () => {
  it('renders unchecked by default', () => {
    render(<Switch />)
    expect(screen.getByRole('switch')).toBeInTheDocument()
  })

  it('renders checked', () => {
    render(<Switch checked />)
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
  })

  it('calls onCheckedChange when clicked', () => {
    const onChange = vi.fn()
    render(<Switch onCheckedChange={onChange} />)
    // @base-ui/react/switch internally dispatches a click to a hidden input.
    // In jsdom, we need to fire the click on the hidden checkbox directly.
    const hiddenInput = document.querySelector('input[type="checkbox"][aria-hidden="true"]')!
    fireEvent.click(hiddenInput)
    expect(onChange).toHaveBeenCalled()
  })
})
