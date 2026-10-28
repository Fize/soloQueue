import { act, createEvent, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ChatInput } from './ChatInput'
import { uploadFile } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  getProjectBranches: vi.fn().mockResolvedValue(['main']),
  uploadFile: vi.fn(),
}))

const mockedUploadFile = vi.mocked(uploadFile)

beforeEach(() => {
  mockedUploadFile.mockReset()
  mockedUploadFile.mockResolvedValue({ name: 'uploaded.png', path: '/uploads/uploaded.png', size: 10 })
  let objectUrlId = 0
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    value: vi.fn(() => `blob:test-${++objectUrlId}`),
  })
  Object.defineProperty(URL, 'revokeObjectURL', {
    configurable: true,
    value: vi.fn(),
  })
})

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
  it('restores an unsent session draft after the input unmounts and remounts', async () => {
    const user = userEvent.setup()
    const first = renderInput({ activeSessionId: 'draft-restore-test' })
    await user.type(screen.getByPlaceholderText('Ask anything...'), 'Keep this draft')
    first.unmount()
    renderInput({ activeSessionId: 'draft-restore-test' })
    expect(screen.getByPlaceholderText('Ask anything...')).toHaveValue('Keep this draft')
  })
  it('keeps drafts separate when switching sessions and clears the accepted draft', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn().mockResolvedValue(true)
    const { rerender } = render(<ChatInput onSend={onSend} onCancel={vi.fn()} streaming={false} delegating={false} disabled={false} activeSessionId="draft-a" />)
    const input = screen.getByPlaceholderText('Ask anything...')
    await user.type(input, 'Draft A')
    rerender(<ChatInput onSend={onSend} onCancel={vi.fn()} streaming={false} delegating={false} disabled={false} activeSessionId="draft-b" />)
    expect(input).toHaveValue('')
    await user.type(input, 'Draft B')
    rerender(<ChatInput onSend={onSend} onCancel={vi.fn()} streaming={false} delegating={false} disabled={false} activeSessionId="draft-a" />)
    expect(input).toHaveValue('Draft A')
    await user.keyboard('{Enter}')
    expect(input).toHaveValue('')
    rerender(<ChatInput onSend={onSend} onCancel={vi.fn()} streaming={false} delegating={false} disabled={false} activeSessionId="draft-b" />)
    expect(input).toHaveValue('Draft B')
  })
  it('does not clear another session draft when an earlier send resolves', async () => {
    const user = userEvent.setup()
    let finish!: (value: boolean) => void
    const onSend = vi.fn(() => new Promise<boolean>(resolve => { finish = resolve }))
    const { rerender } = render(<ChatInput onSend={onSend} onCancel={vi.fn()} streaming={false} delegating={false} disabled={false} activeSessionId="pending-a" />)
    const input = screen.getByPlaceholderText('Ask anything...')
    await user.type(input, 'Send A{Enter}')
    rerender(<ChatInput onSend={onSend} onCancel={vi.fn()} streaming={false} delegating={false} disabled={false} activeSessionId="pending-b" />)
    await user.type(input, 'Draft B')
    await act(async () => finish(true))
    expect(input).toHaveValue('Draft B')
  })
  it('does not render route badges when task and model props are supplied', () => {
    renderInput({
      taskLevel: 'route-task-sentinel',
      modelName: 'route-model-sentinel',
    })

    expect(screen.queryByText('route-task-sentinel')).not.toBeInTheDocument()
    expect(screen.queryByText('route-model-sentinel')).not.toBeInTheDocument()
  })

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

describe('ChatInput image attachments', () => {
  it('opens the image chooser and uploads selected images only', async () => {
    const user = userEvent.setup()
    const { container } = renderInput()
    const fileInput = container.querySelector('input[type="file"]') as HTMLInputElement
    const clickSpy = vi.spyOn(fileInput, 'click')
    const image = new File(['image'], 'screenshot.png', { type: 'image/png' })
    const secondImage = new File(['image-2'], 'diagram.jpg', { type: 'image/jpeg' })
    const text = new File(['text'], 'notes.txt', { type: 'text/plain' })

    await user.click(screen.getByTitle('Add context'))
    expect(clickSpy).toHaveBeenCalled()

    fireEvent.change(fileInput, { target: { files: [image, secondImage, text] } })

    expect(mockedUploadFile).toHaveBeenCalledTimes(2)
    expect(mockedUploadFile).toHaveBeenCalledWith(image, undefined)
    expect(mockedUploadFile).toHaveBeenCalledWith(secondImage, undefined)
    expect(await screen.findAllByAltText('preview')).toHaveLength(2)
  })

  it('handles image drops, prevents browser navigation, and ignores non-images', () => {
    const { getByPlaceholderText } = renderInput()
    const image = new File(['image'], 'screenshot.png', { type: 'image/png' })
    const text = new File(['text'], 'notes.txt', { type: 'text/plain' })
    const textarea = getByPlaceholderText('Ask anything...')

    const dropEvent = createEvent.drop(textarea, {
      dataTransfer: {
        types: ['Files'],
        files: [image, text],
      },
    })
    fireEvent(textarea, dropEvent)

    expect(dropEvent.defaultPrevented).toBe(true)
    expect(mockedUploadFile).toHaveBeenCalledTimes(1)
    expect(mockedUploadFile).toHaveBeenCalledWith(image, undefined)
  })
})
