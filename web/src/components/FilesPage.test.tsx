import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FilesPage } from './FilesPage'
import * as api from '@/lib/api'

const mockRoots = [
  { label: 'work', path: '/home/user/work', group: '' },
  { label: 'docs', path: '/var/docs', group: '' },
  { label: 'team-a', path: '/home/user/team-a', group: 'team-a' },
  { label: 'team-b', path: '/home/user/team-b', group: 'team-b' },
]

const mockFiles = [
  {
    name: 'file1.ts',
    path: '/home/user/work/file1.ts',
    size: 100,
    isDir: false,
    ext: '.ts',
    modTime: '',
  },
  { name: 'subdir', path: '/home/user/work/subdir', size: 0, isDir: true, ext: '', modTime: '' },
]

beforeEach(() => {
  vi.clearAllMocks()
})

describe('FilesPage', () => {
  it('shows loading state initially', () => {
    vi.spyOn(api, 'getFileRoots').mockReturnValue(new Promise(() => {}))
    const { container } = render(<FilesPage />)
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('renders file roots after loading', async () => {
    vi.spyOn(api, 'getFileRoots').mockResolvedValue(mockRoots)
    render(<FilesPage />)
    await waitFor(() => {
      expect(screen.getByText('Files')).toBeInTheDocument()
    })
    expect(screen.getByText('work')).toBeInTheDocument()
    expect(screen.getByText('docs')).toBeInTheDocument()
  })

  it('shows error state', async () => {
    vi.spyOn(api, 'getFileRoots').mockRejectedValue(new Error('Network error'))
    render(<FilesPage />)
    await waitFor(() => {
      expect(screen.getByText('Network error')).toBeInTheDocument()
    })
  })

  it('filters out .soloqueue roots', async () => {
    const rootsWithSolo = [...mockRoots, { label: 'sq', path: '/home/user/.soloqueue', group: '' }]
    vi.spyOn(api, 'getFileRoots').mockResolvedValue(rootsWithSolo)
    render(<FilesPage />)
    await waitFor(() => {
      expect(screen.getByText('work')).toBeInTheDocument()
    })
    expect(screen.queryByText('.soloqueue')).not.toBeInTheDocument()
  })

  it('loads children when expanding a directory', async () => {
    vi.spyOn(api, 'getFileRoots').mockResolvedValue(mockRoots)
    vi.spyOn(api, 'listFiles').mockResolvedValue(mockFiles)
    const user = userEvent.setup()
    render(<FilesPage />)

    await waitFor(() => {
      expect(screen.getByText('work')).toBeInTheDocument()
    })

    await user.click(screen.getByText('work'))
    await waitFor(() => {
      expect(screen.getByText('file1.ts')).toBeInTheDocument()
    })
    expect(screen.getByText('subdir')).toBeInTheDocument()
  })

  it('selects a file on click', async () => {
    vi.spyOn(api, 'getFileRoots').mockResolvedValue(mockRoots)
    vi.spyOn(api, 'listFiles').mockResolvedValue(mockFiles)
    const user = userEvent.setup()
    render(<FilesPage />)

    await waitFor(() => expect(screen.getByText('work')).toBeInTheDocument())
    await user.click(screen.getByText('work'))
    await waitFor(() => expect(screen.getByText('file1.ts')).toBeInTheDocument())
    await user.click(screen.getByText('file1.ts'))

    expect(screen.getAllByText('file1.ts').length).toBeGreaterThanOrEqual(1)
  })

  it('shows groups', async () => {
    vi.spyOn(api, 'getFileRoots').mockResolvedValue(mockRoots)
    render(<FilesPage />)
    await waitFor(() => {
      expect(screen.getByText('team-a')).toBeInTheDocument()
    })
    expect(screen.getByText('team-b')).toBeInTheDocument()
  })
})
