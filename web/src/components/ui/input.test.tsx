import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Input } from './input'

describe('Input', () => {
  it('renders with label', () => {
    render(<Input label="Name" />)
    expect(screen.getByLabelText('Name')).toBeInTheDocument()
  })

  it('shows error message', () => {
    render(<Input label="Email" error="Required" />)
    expect(screen.getByText('Required')).toBeInTheDocument()
  })

  it('calls onChange', async () => {
    const user = (await import('@testing-library/user-event')).default.setup()
    const onChange = vi.fn()
    render(<Input label="Name" onChange={onChange} />)
    await user.type(screen.getByLabelText('Name'), 'a')
    expect(onChange).toHaveBeenCalled()
  })

  it('renders without label', () => {
    const { container } = render(<Input placeholder="no label" />)
    expect(container.querySelector('input')).toBeInTheDocument()
  })
})
