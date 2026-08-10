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

  it('keeps a restored absolute path associated with its tilde-configured project', () => {
    const onProjectChange = vi.fn()
    const tildeProject = { ...project, path: '~/projects/one/' }

    renderInput({
      projects: [tildeProject],
      teamProjectsMap: { dev: [tildeProject] },
      selectedProjectPath: '/Users/developer/projects/one',
      onProjectChange,
    })

    expect(screen.getByRole('button', { name: 'Project One' })).toBeInTheDocument()
    expect(onProjectChange).not.toHaveBeenCalled()
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

  it('submits ordinary text while a session is streaming', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn()
    renderInput({ onSend, streaming: true, processing: true })

    await user.type(screen.getByPlaceholderText('Ask anything...'), 'Follow up{enter}')

    expect(onSend).toHaveBeenCalledWith(
      'Follow up',
      undefined,
      'dev',
      undefined,
      undefined,
    )
  })

  it('keeps clear and compact commands local while a session is active', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn()
    renderInput({ onSend, streaming: true, processing: true })
    const input = screen.getByPlaceholderText('Ask anything...')

    await user.type(input, '/clear{enter}')
    await user.clear(input)
    await user.type(input, '/compact{enter}')

    expect(onSend).not.toHaveBeenCalled()
  })
})
