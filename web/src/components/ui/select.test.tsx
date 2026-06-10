import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Select } from './select'

describe('Select', () => {
  const options = [
    { value: 'a', label: 'Option A' },
    { value: 'b', label: 'Option B' },
  ]

  it('renders with label and trigger', () => {
    render(<Select label="Choose" options={options} />)
    const label = screen.getByText('Choose')
    expect(label).toBeInTheDocument()
    expect(screen.getByText('Select...')).toBeInTheDocument()
  })

  it('calls onChange when an option is selected via the popup', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<Select label="Choose" options={options} onChange={onChange} />)

    // Click the trigger (role="combobox") to open the popup
    const trigger = screen.getByRole('combobox')
    await user.click(trigger)

    // Click the option to select it
    const optionB = screen.getByText('Option B')
    await user.click(optionB)

    expect(onChange).toHaveBeenCalledWith('b')
  })

  it('shows selected value', () => {
    render(<Select label="Choose" options={options} value="a" />)
    expect(screen.getByText('Option A')).toBeInTheDocument()
  })

  it('disables the select', () => {
    render(<Select label="Choose" options={options} disabled />)
    expect(screen.getByRole('combobox')).toBeDisabled()
  })
})
