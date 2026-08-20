import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AgentMemoriesTab, AgentReflectionsTab } from './AgentMemoriesTab'
import type { MemoryRecord } from '@/types'

function makeMem(overrides: Partial<MemoryRecord> = {}): MemoryRecord {
  return {
    round: 1,
    record_type: 'observation',
    content: 'Saw a cat',
    importance: 5,
    simulated_time: '2026-07-10T08:00:00Z',
    location: 'Park',
    ...overrides,
  }
}

describe('AgentMemoriesTab', () => {
  it('shows loading spinner', () => {
    render(<AgentMemoriesTab memories={null} memoriesLoading={true} memoriesError={null} />)
    expect(screen.getByText('Loading memories...')).toBeInTheDocument()
  })

  it('shows error message', () => {
    render(<AgentMemoriesTab memories={null} memoriesLoading={false} memoriesError="DB error" />)
    expect(screen.getByText('DB error')).toBeInTheDocument()
  })

  it('shows empty state for null', () => {
    render(<AgentMemoriesTab memories={null} memoriesLoading={false} memoriesError={null} />)
    expect(screen.getByText('No memory records found.')).toBeInTheDocument()
  })

  it('shows empty state for empty array', () => {
    render(<AgentMemoriesTab memories={[]} memoriesLoading={false} memoriesError={null} />)
    expect(screen.getByText('No memory records found.')).toBeInTheDocument()
  })

  it('renders memory cards', () => {
    const memories: MemoryRecord[] = [
      makeMem({ round: 1, record_type: 'observation', content: 'Saw a cat', location: 'Park' }),
      makeMem({ round: 2, record_type: 'action', content: 'Fed the cat', location: 'Home', importance: 8 }),
    ]
    render(<AgentMemoriesTab memories={memories} memoriesLoading={false} memoriesError={null} />)
    // Search input and filter dropdown should exist
    expect(screen.getByPlaceholderText('Search memories...')).toBeInTheDocument()
    expect(screen.getByText('All Types')).toBeInTheDocument()
    // Reverse order: round 2 first
    const cards = screen.getAllByText(/Fed the cat|Saw a cat/)
    expect(cards[0]).toHaveTextContent('Fed the cat')
    expect(cards[1]).toHaveTextContent('Saw a cat')
  })

  it('filters by search text', async () => {
    const user = userEvent.setup()
    const memories: MemoryRecord[] = [
      makeMem({ round: 1, content: 'Saw a cat', location: 'Park' }),
      makeMem({ round: 2, content: 'Saw a dog', location: 'Home' }),
    ]
    render(<AgentMemoriesTab memories={memories} memoriesLoading={false} memoriesError={null} />)

    const searchInput = screen.getByPlaceholderText('Search memories...')
    await user.type(searchInput, 'dog')

    expect(screen.getByText('Saw a dog')).toBeInTheDocument()
    expect(screen.queryByText('Saw a cat')).not.toBeInTheDocument()
  })

  it('filters by search text matching location', async () => {
    const user = userEvent.setup()
    const memories: MemoryRecord[] = [
      makeMem({ round: 1, content: 'Walked around', location: 'Park' }),
      makeMem({ round: 2, content: 'Ate lunch', location: 'Cafe' }),
    ]
    render(<AgentMemoriesTab memories={memories} memoriesLoading={false} memoriesError={null} />)

    const searchInput = screen.getByPlaceholderText('Search memories...')
    await user.type(searchInput, 'park')

    expect(screen.getByText('Walked around')).toBeInTheDocument()
    expect(screen.queryByText('Ate lunch')).not.toBeInTheDocument()
  })

  it('filters by record type', async () => {
    const user = userEvent.setup()
    const memories: MemoryRecord[] = [
      makeMem({ round: 1, record_type: 'observation', content: 'Saw something' }),
      makeMem({ round: 2, record_type: 'action', content: 'Did something' }),
    ]
    render(<AgentMemoriesTab memories={memories} memoriesLoading={false} memoriesError={null} />)

    const select = screen.getByRole('combobox')
    await user.selectOptions(select, 'action')

    expect(screen.getByText('Did something')).toBeInTheDocument()
    expect(screen.queryByText('Saw something')).not.toBeInTheDocument()
  })

  it('shows importance badge with color coding', () => {
    const memories: MemoryRecord[] = [
      makeMem({ importance: 9, content: 'Very important' }),
      makeMem({ importance: 5, content: 'Moderate' }),
      makeMem({ importance: 2, content: 'Low' }),
    ]
    render(<AgentMemoriesTab memories={memories} memoriesLoading={false} memoriesError={null} />)
    // Importance renders as "Importance: X.X"
    expect(screen.getByText('Importance: 9.0')).toBeInTheDocument()
    expect(screen.getByText('Importance: 5.0')).toBeInTheDocument()
    expect(screen.getByText('Importance: 2.0')).toBeInTheDocument()
  })
})

describe('AgentReflectionsTab', () => {
  it('shows loading spinner', () => {
    render(<AgentReflectionsTab reflections={null} reflectionsLoading={true} reflectionsError={null} />)
    expect(screen.getByText('Loading higher-order reflections...')).toBeInTheDocument()
  })

  it('shows error message', () => {
    render(<AgentReflectionsTab reflections={null} reflectionsLoading={false} reflectionsError="Load failed" />)
    expect(screen.getByText('Load failed')).toBeInTheDocument()
  })

  it('shows empty state', () => {
    render(<AgentReflectionsTab reflections={[]} reflectionsLoading={false} reflectionsError={null} />)
    expect(screen.getByText('No reflections generated yet. Reflections are periodically triggered during simulation runtime.')).toBeInTheDocument()
  })

  it('renders reflection cards', () => {
    const reflections: MemoryRecord[] = [
      makeMem({ round: 1, record_type: 'reflection', content: '**Insight:** good day', importance: 7 }),
    ]
    render(<AgentReflectionsTab reflections={reflections} reflectionsLoading={false} reflectionsError={null} />)
    expect(screen.getByText('Agent Reflections & Insights')).toBeInTheDocument()
    // react-markdown should render bold text
    expect(screen.getByText('Insight:')).toBeInTheDocument()
  })
})
