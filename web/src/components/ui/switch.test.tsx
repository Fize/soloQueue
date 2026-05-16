import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
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

  it('calls onCheckedChange when clicked', async () => {
    const user = (await import('@testing-library/user-event')).default.setup()
    const onChange = vi.fn()
    render(<Switch onCheckedChange={onChange} />)
    await user.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenCalled()
  })
})
