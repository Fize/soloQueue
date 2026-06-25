import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Label } from './label'

describe('Label', () => {
  it('renders children', () => {
    render(<Label>Name</Label>)
    expect(screen.getByText('Name')).toBeInTheDocument()
  })

  it('forwards htmlFor', () => {
    render(<Label htmlFor="email">Email</Label>)
    expect(screen.getByText('Email')).toHaveAttribute('for', 'email')
  })
})
