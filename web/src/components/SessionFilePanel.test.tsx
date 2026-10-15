import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SessionFilePanel } from './SessionFilePanel'
import {
  INSPECTOR_EXPANDED_DIRECTORIES_MAX,
  useInspectorStateStore,
} from '@/stores/inspectorStateStore'

const mocks = vi.hoisted(() => ({
  listFiles: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  listFiles: mocks.listFiles,
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('./FileContentView', () => ({
  FileContentView: ({ path }: { path: string | null }) => <div>{path}</div>,
}))

vi.mock('@/components/ui/scroll-area', () => ({
  ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))

beforeEach(() => {
  localStorage.clear()
  useInspectorStateStore.setState({ sessions: {} })
  vi.clearAllMocks()
  mocks.listFiles.mockResolvedValue([
    {
      name: 'package.json',
      path: '/project/package.json',
      size: 10,
      isDir: false,
      ext: '.json',
    },
  ])
})

describe('SessionFilePanel', () => {
  it('applies hover styling only while a file row is inactive', async () => {
    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    const fileRow = await screen.findByRole('button', { name: 'package.json' })
    expect(fileRow).toHaveClass('hover:bg-muted/50')
    expect(fileRow).not.toHaveClass('bg-accent', 'text-accent-foreground')

    fireEvent.click(fileRow)

    const activeFileRow = screen.getByRole('button', { name: 'package.json' })
    expect(activeFileRow).toHaveClass('bg-accent', 'text-accent-foreground')
    expect(activeFileRow).not.toHaveClass('hover:bg-muted/50')
  })

  it('restores a project-relative selected file without persisting its contents', async () => {
    useInspectorStateStore.getState().updateSession('l2:files', {
      projectPath: '/project',
      selectedFile: 'package.json',
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    const fileRow = await screen.findByRole('button', { name: 'package.json' })
    expect(fileRow).toHaveClass('bg-accent', 'text-accent-foreground')
    expect(screen.getByText('/project/package.json')).toBeInTheDocument()
    expect(JSON.parse(localStorage.getItem('soloqueue_inspector_state') || '{}')).not.toHaveProperty(
      'sessions.l2:files.children',
    )
  })

  it('reloads persisted expanded directories without storing fetched children', async () => {
    mocks.listFiles.mockImplementation(async (path: string) => {
      if (path === '/project') {
        return [
          { name: 'src', path: '/project/src', size: 0, isDir: true, ext: '' },
        ]
      }
      return [
        { name: 'main.ts', path: '/project/src/main.ts', size: 10, isDir: false, ext: '.ts' },
      ]
    })
    useInspectorStateStore.getState().updateSession('l2:files', {
      projectPath: '/project',
      expandedDirectories: ['src'],
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(await screen.findByRole('button', { name: 'main.ts' })).toBeInTheDocument()
    expect(mocks.listFiles).toHaveBeenCalledWith('/project/src')
    expect(JSON.parse(localStorage.getItem('soloqueue_inspector_state') || '{}')).not.toHaveProperty(
      'sessions.l2:files.children',
    )
  })

  it('discards file API nodes that resolve outside the project root', async () => {
    mocks.listFiles.mockResolvedValue([
      { name: 'safe.ts', path: '/project/safe.ts', size: 10, isDir: false, ext: '.ts' },
      { name: 'outside.ts', path: '/outside.ts', size: 10, isDir: false, ext: '.ts' },
      { name: 'traversal', path: '/project/../outside', size: 0, isDir: true, ext: '' },
      { name: 'drive.ts', path: 'C:\\outside.ts', size: 10, isDir: false, ext: '.ts' },
    ])

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(await screen.findByRole('button', { name: 'safe.ts' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'outside.ts' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'traversal' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'drive.ts' })).not.toBeInTheDocument()
  })

  it('defensively bounds restored directory request fan-out', async () => {
    const expandedDirectories = Array.from(
      { length: INSPECTOR_EXPANDED_DIRECTORIES_MAX + 5 },
      (_, index) => `src/${index}`,
    )
    useInspectorStateStore.setState({
      sessions: {
        'l2:files': {
          open: true,
          tab: 'files',
          selectedPlan: '',
          panelWidth: 500,
          projectPath: '/project',
          expandedDirectories,
          selectedFile: null,
          lastActivity: Date.now(),
        },
      },
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    await screen.findByRole('button', { name: 'package.json' })
    expect(mocks.listFiles).toHaveBeenCalledTimes(INSPECTOR_EXPANDED_DIRECTORIES_MAX + 1)
    expect(mocks.listFiles).not.toHaveBeenCalledWith(
      `/project/src/${INSPECTOR_EXPANDED_DIRECTORIES_MAX}`,
    )
  })
})
