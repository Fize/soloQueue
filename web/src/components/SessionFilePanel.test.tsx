import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SessionFilePanel } from './SessionFilePanel'

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
    render(<SessionFilePanel projectPath="/project" panelWidth={500} />)

    const fileRow = await screen.findByRole('button', { name: 'package.json' })
    expect(fileRow).toHaveClass('hover:bg-muted/50')
    expect(fileRow).not.toHaveClass('bg-accent', 'text-accent-foreground')

    fireEvent.click(fileRow)

    const activeFileRow = screen.getByRole('button', { name: 'package.json' })
    expect(activeFileRow).toHaveClass('bg-accent', 'text-accent-foreground')
    expect(activeFileRow).not.toHaveClass('hover:bg-muted/50')
  })
})
