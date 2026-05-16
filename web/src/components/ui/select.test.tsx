import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Select } from './select'

describe('Select', () => {
  const options = [
    { value: 'a', label: 'Option A' },
    { value: 'b', label: 'Option B' },
  ]

  it('renders with label and options', () => {
    render(<Select label="Choose" options={options} />)
    expect(screen.getByLabelText('Choose')).toBeInTheDocument()
    expect(screen.getByText('Option A')).toBeInTheDocument()
    expect(screen.getByText('Option B')).toBeInTheDocument()
  })

  it('calls onChange with the selected value', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<Select label="Choose" options={options} onChange={onChange} />)
    await user.selectOptions(screen.getByLabelText('Choose'), 'b')
    expect(onChange).toHaveBeenCalledWith('b')
  })
})
