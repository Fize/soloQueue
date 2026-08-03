import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ChatInput } from './ChatInput'

vi.mock('@/lib/api', () => ({
  getProjectBranches: vi.fn().mockResolvedValue(['main']),
  uploadFile: vi.fn(),
}))

const project = {
  id: 'project-1',
  name: 'Project One',
  path: '/projects/one',
  description: '',
  created_at: '',
  updated_at: '',
}

function renderInput(overrides: Partial<React.ComponentProps<typeof ChatInput>> = {}) {
  const props: React.ComponentProps<typeof ChatInput> = {
    onSend: vi.fn(),
    onCancel: vi.fn(),
    streaming: false,
    delegating: false,
    disabled: false,
    showL2Selectors: true,
    groups: ['dev'],
    projects: [project],
    teamProjectsMap: { dev: [project] },
    selectedGroup: 'dev',
    selectedProjectPath: '',
    onGroupChange: vi.fn(),
    onProjectChange: vi.fn(),
    ...overrides,
  }

  return { ...render(<ChatInput {...props} />), props }
}

describe('ChatInput workspace selector', () => {
  it('keeps local selected by default even when projects are available', async () => {
    const user = userEvent.setup()
    const onProjectChange = vi.fn()
    renderInput({ onProjectChange })

    expect(screen.getByRole('button', { name: 'Local' })).toBeInTheDocument()
    expect(onProjectChange).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Local' }))
    expect(screen.getByRole('button', { name: 'Project One' })).toBeInTheDocument()
  })

  it('allows switching an existing project selection back to local', async () => {
    const user = userEvent.setup()
    const onProjectChange = vi.fn()
    renderInput({ selectedProjectPath: project.path, onProjectChange })

    await user.click(screen.getByRole('button', { name: 'Project One' }))
    await user.click(screen.getByRole('button', { name: 'Local' }))

    expect(onProjectChange).toHaveBeenCalledWith('')
  })

  it('sends no project path when local is selected', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn()
    renderInput({ onSend })

    await user.type(screen.getByPlaceholderText('Ask anything...'), 'Use the default workspace{enter}')

    expect(onSend).toHaveBeenCalledWith(
      'Use the default workspace',
      undefined,
      'dev',
      undefined,
      undefined,
    )
  })
})
