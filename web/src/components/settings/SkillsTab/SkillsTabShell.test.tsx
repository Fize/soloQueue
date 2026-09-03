import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useSyncExternalStore } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  fetchSkillDetail: vi.fn(),
  fetchSkillFiles: vi.fn(),
  fetchSkills: vi.fn(),
}))

const skill = {
  id: 'calendar',
  name: 'calendar',
  description: 'Calendar skill',
  when_to_use: '',
  user_invocable: true,
  disable_model_invocation: false,
  context: '',
  agent: '',
  file_path: '/tmp/calendar/SKILL.md',
  allowed_tools: [],
  triggers: [],
  body: 'updated instructions',
}

const secondSkill = {
  ...skill,
  id: 'notes',
  name: 'notes',
  description: 'Notes skill',
  file_path: '/tmp/notes/SKILL.md',
  body: 'notes instructions',
}

const storeState = {
  skills: { skills: [skill], total: 1 },
  skillsLoading: false,
  fetchSkills: mocks.fetchSkills,
}
let storeVersion = 0
const storeListeners = new Set<() => void>()

function publishSkills(nextSkills: typeof storeState.skills) {
  storeState.skills = nextSkills
  storeVersion += 1
  for (const listener of storeListeners) listener()
}

vi.mock('@/lib/api', () => mocks)
vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/toolsAndSkillsStore', () => ({
  useToolsAndSkillsStore: Object.assign(
    (selector: (state: typeof storeState) => unknown) => {
      useSyncExternalStore(
        (listener) => {
          storeListeners.add(listener)
          return () => storeListeners.delete(listener)
        },
        () => storeVersion,
        () => storeVersion
      )
      return selector(storeState)
    },
    { getState: () => storeState }
  ),
}))
vi.mock('@/components/ui/markdown-preview', () => ({
  MarkdownPreview: ({ content }: { content: string }) => <div>{content}</div>,
}))

describe('SkillsTab refresh', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    storeState.skills = { skills: [skill], total: 1 }
    storeVersion = 0
    storeListeners.clear()
    mocks.fetchSkillDetail.mockResolvedValue(skill)
    mocks.fetchSkillFiles.mockResolvedValue({ files: [] })
    mocks.fetchSkills.mockResolvedValue(undefined)
  })

  it('reloads expanded details and files after refreshing the installed list', async () => {
    const { SkillsTab } = await import('./SkillsTabShell')
    render(<SkillsTab />)

    fireEvent.click(screen.getByRole('button', { name: /calendar/ }))
    await waitFor(() => expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(1))
    expect(mocks.fetchSkillFiles).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'common.refresh' }))

    await waitFor(() => expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(2))
    expect(mocks.fetchSkillFiles).toHaveBeenCalledTimes(2)
    expect(mocks.fetchSkills).toHaveBeenCalledTimes(2)
  })

  it('does not render the skill management guidance banner', async () => {
    const { SkillsTab } = await import('./SkillsTabShell')
    render(<SkillsTab />)

    expect(screen.queryByText('skills.installedOnlyDesc')).not.toBeInTheDocument()
    expect(screen.queryByText('skills.clawhubHint')).not.toBeInTheDocument()
  })

  it('ignores an older detail response that arrives after refresh', async () => {
    let resolveOldDetail!: (value: typeof skill) => void
    const oldDetail = new Promise<typeof skill>((resolve) => {
      resolveOldDetail = resolve
    })
    mocks.fetchSkillDetail
      .mockImplementationOnce(() => oldDetail)
      .mockResolvedValueOnce({ ...skill, body: 'new instructions' })

    const { SkillsTab } = await import('./SkillsTabShell')
    render(<SkillsTab />)
    fireEvent.click(screen.getByRole('button', { name: /calendar/ }))
    await waitFor(() => expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: 'common.refresh' }))
    expect(await screen.findByText('new instructions')).toBeInTheDocument()

    resolveOldDetail({ ...skill, body: 'old instructions' })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(screen.queryByText('old instructions')).not.toBeInTheDocument()
    expect(screen.getByText('new instructions')).toBeInTheDocument()
  })

  it('keeps a newer expansion selected when it changes during refresh', async () => {
    let resolveRefresh!: () => void
    const refresh = new Promise<void>((resolve) => {
      resolveRefresh = resolve
    })
    let resolveNotes!: (value: typeof secondSkill) => void
    const notesDetail = new Promise<typeof secondSkill>((resolve) => {
      resolveNotes = resolve
    })

    mocks.fetchSkills
      .mockImplementationOnce(async () => undefined)
      .mockImplementationOnce(() => refresh)
    mocks.fetchSkillDetail.mockResolvedValueOnce(skill).mockImplementationOnce(() => notesDetail)

    const { SkillsTab } = await import('./SkillsTabShell')
    render(<SkillsTab />)

    fireEvent.click(screen.getByRole('button', { name: /calendar/ }))
    await waitFor(() => expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: 'common.refresh' }))
    publishSkills({ skills: [skill, secondSkill], total: 2 })
    fireEvent.click(await screen.findByRole('button', { name: /notes/ }))
    await waitFor(() => expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(2))

    resolveRefresh()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(2)

    resolveNotes(secondSkill)
    expect(await screen.findByText('notes instructions')).toBeInTheDocument()
    expect(screen.queryByText('updated instructions')).not.toBeInTheDocument()
  })

  it('collapses an expanded skill that disappears from the refreshed list', async () => {
    const { SkillsTab } = await import('./SkillsTabShell')
    mocks.fetchSkills
      .mockImplementationOnce(async () => undefined)
      .mockImplementationOnce(async () => {
        publishSkills({ skills: [], total: 0 })
      })

    render(<SkillsTab />)
    fireEvent.click(screen.getByRole('button', { name: /calendar/ }))
    await waitFor(() => expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: 'common.refresh' }))

    await waitFor(() => {
      expect(screen.queryByRole('button', { name: /calendar/ })).not.toBeInTheDocument()
      expect(screen.queryByText('updated instructions')).not.toBeInTheDocument()
    })
    expect(mocks.fetchSkillDetail).toHaveBeenCalledTimes(1)

    publishSkills({ skills: [skill], total: 1 })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /calendar/ })).toBeInTheDocument()
    )
    expect(screen.queryByText('updated instructions')).not.toBeInTheDocument()
  })
})
