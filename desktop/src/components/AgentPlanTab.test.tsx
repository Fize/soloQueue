import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AgentPlanTab } from './AgentPlanTab'

describe('AgentPlanTab', () => {
  it('shows loading spinner when planLoading is true', () => {
    render(<AgentPlanTab plan={null} planLoading={true} planError={null} />)
    expect(screen.getByText('Loading daily plan...')).toBeInTheDocument()
  })

  it('shows error message when planError is set', () => {
    render(<AgentPlanTab plan={null} planLoading={false} planError="Failed to load plan" />)
    expect(screen.getByText('Failed to load plan')).toBeInTheDocument()
  })

  it('shows empty state when plan is null', () => {
    render(<AgentPlanTab plan={null} planLoading={false} planError={null} />)
    expect(screen.getByText(/No daily schedule plan found/)).toBeInTheDocument()
  })

  it('shows empty state when schedule is empty array', () => {
    render(<AgentPlanTab plan={{ schedule: [] }} planLoading={false} planError={null} />)
    expect(screen.getByText(/No daily schedule plan found/)).toBeInTheDocument()
  })

  it('renders schedule items', () => {
    const plan = {
      schedule: [
        {
          activity: 'Morning walk',
          location: 'Park',
          description: 'Take a walk',
          status: 'completed',
          start_time: '2026-07-10T08:00:00Z',
          end_time: '2026-07-10T09:00:00Z',
        },
        {
          activity: 'Work meeting',
          location: 'Office',
          description: undefined,
          status: 'in_progress',
          start_time: '2026-07-10T10:00:00Z',
          end_time: '2026-07-10T11:30:00Z',
        },
      ],
    }
    render(<AgentPlanTab plan={plan} planLoading={false} planError={null} />)
    expect(screen.getByText("Today's Schedule")).toBeInTheDocument()
    expect(screen.getByText('Morning walk')).toBeInTheDocument()
    expect(screen.getByText('Work meeting')).toBeInTheDocument()
    expect(screen.getByText('📍 Park')).toBeInTheDocument()
    expect(screen.getByText('Take a walk')).toBeInTheDocument()
    // work meeting has no description — should not render
    const descriptions = screen.queryAllByText(/[a-zA-Z].*/, { selector: 'div.italic' })
    expect(descriptions).toHaveLength(1)
  })

  it('renders status labels for each status type', () => {
    const plan = {
      schedule: [
        { activity: 'a', location: 'l', description: null, status: 'pending', start_time: '2026-07-10T08:00:00Z', end_time: '2026-07-10T09:00:00Z' },
        { activity: 'b', location: 'l', description: null, status: 'in_progress', start_time: '2026-07-10T09:00:00Z', end_time: '2026-07-10T10:00:00Z' },
        { activity: 'c', location: 'l', description: null, status: 'completed', start_time: '2026-07-10T10:00:00Z', end_time: '2026-07-10T11:00:00Z' },
        { activity: 'd', location: 'l', description: null, status: 'cancelled', start_time: '2026-07-10T11:00:00Z', end_time: '2026-07-10T12:00:00Z' },
      ],
    }
    render(<AgentPlanTab plan={plan} planLoading={false} planError={null} />)
    expect(screen.getByText('Pending')).toBeInTheDocument()
    expect(screen.getByText('In Progress')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
    expect(screen.getByText('Cancelled')).toBeInTheDocument()
  })
})
