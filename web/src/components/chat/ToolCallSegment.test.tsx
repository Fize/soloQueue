import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { ChatMessage } from '@/types'
import { extractImagePaths, ImageResultPreviews, ToolCallSegment } from './ToolCallSegment'

type ToolSegment = Extract<ChatMessage['segments'][number], { type: 'tool_call' }>

function toolSegment(over: Partial<ToolSegment> = {}): ToolSegment {
  return { type: 'tool_call', callId: 'call-1', name: 'Bash', args: '{}', done: true, ...over }
}

describe('ToolCallSegment tool sources', () => {
  it('marks lsp calls with the LSP identity', () => {
    const { container } = render(
      <ToolCallSegment segment={toolSegment({ name: 'lsp__hover', args: '{"file":"/a/b.ts"}' })} />
    )
    expect(screen.getByText('LSP')).toBeInTheDocument()
    expect(container.querySelector('[class*="bg-tool-lsp"]')).not.toBeNull()
    expect(screen.getByText(/hover\s+b\.ts/)).toBeInTheDocument()
  })

  it('marks mcp calls with the MCP identity and a server/tool header', () => {
    const { container } = render(
      <ToolCallSegment segment={toolSegment({ name: 'mcp__playwright__browser_click' })} />
    )
    expect(screen.getByText('MCP')).toBeInTheDocument()
    expect(container.querySelector('[class*="bg-tool-mcp"]')).not.toBeNull()
    expect(screen.getByText('playwright/browser_click')).toBeInTheDocument()
  })

  it('marks skill calls with the SKILL identity', () => {
    const { container } = render(
      <ToolCallSegment
        segment={toolSegment({ name: 'Skill', args: '{"skill":"qa-suite","args":"web"}' })}
      />
    )
    expect(screen.getByText('SKILL')).toBeInTheDocument()
    expect(container.querySelector('[class*="bg-tool-skill"]')).not.toBeNull()
    expect(screen.getByText('qa-suite (web)')).toBeInTheDocument()
  })

  it('leaves builtin calls unlabeled and without a colored rail', () => {
    const { container } = render(<ToolCallSegment segment={toolSegment({ name: 'Bash' })} />)
    expect(screen.queryByText('LSP')).toBeNull()
    expect(screen.queryByText('MCP')).toBeNull()
    expect(screen.queryByText('SKILL')).toBeNull()
    expect(container.querySelector('[class*="bg-tool-"]')).toBeNull()
    expect(screen.getByText('Bash')).toBeInTheDocument()
  })

  it('drops source colors inside user bubbles', () => {
    const { container } = render(
      <ToolCallSegment isUser segment={toolSegment({ name: 'mcp__playwright__browser_click' })} />
    )
    expect(container.querySelector('[class*="bg-tool-mcp"]')).toBeNull()
    expect(screen.getByText('MCP')).toBeInTheDocument()
  })
})

describe('ToolCallSegment statuses', () => {
  it('shows a spinner while running', () => {
    const { container } = render(
      <ToolCallSegment segment={toolSegment({ name: 'lsp__hover', done: false })} />
    )
    expect(container.querySelector('.animate-spin')).not.toBeNull()
  })

  it('shows an alert icon and destructive status text on error without tinting the rail', () => {
    const { container } = render(
      <ToolCallSegment segment={toolSegment({ name: 'lsp__hover', error: 'boom' })} />
    )
    expect(container.querySelector('[class*="text-destructive"]')).not.toBeNull()
    expect(screen.getByText('Failed')).toHaveClass('text-destructive/80')
    expect(container.querySelector('[class*="bg-tool-lsp"]')).not.toBeNull()
  })

  it('shows a neutral dot on completion instead of a green one', () => {
    const { container } = render(<ToolCallSegment segment={toolSegment({ name: 'Bash' })} />)
    expect(container.querySelector('[class*="bg-success"]')).toBeNull()
    expect(container.querySelector('[class*="bg-muted-foreground/50"]')).not.toBeNull()
    expect(screen.getByText('Done')).not.toHaveClass('text-destructive/80')
  })
})

describe('extractImagePaths', () => {
  const result = JSON.stringify({ status: 'completed', local_paths: ['/tmp/result.png'] })

  it('recognizes the unified ImageTool result', () => {
    expect(extractImagePaths(result, 'ImageTool')).toEqual(['/tmp/result.png'])
  })

  it('does not special-case other tool names', () => {
    expect(extractImagePaths(result, 'LegacyImageTool')).toEqual([])
  })

  it('falls back to remote image URLs when local persistence failed', () => {
    const remoteResult = JSON.stringify({
      status: 'completed',
      local_paths: [],
      image_urls: ['https://cdn.example.com/result.png'],
    })
    expect(extractImagePaths(remoteResult, 'ImageTool')).toEqual([
      'https://cdn.example.com/result.png',
    ])
  })

  it('renders remote image URLs directly', () => {
    const remoteResult = JSON.stringify({
      status: 'completed',
      image_urls: ['https://cdn.example.com/result.png'],
    })
    render(<ImageResultPreviews result={remoteResult} toolName="ImageTool" />)
    const image = screen.getByRole('img', { name: 'Generated image 1' })
    expect(image).toHaveAttribute('src', 'https://cdn.example.com/result.png')
    expect(image.closest('a')).toHaveAttribute('href', 'https://cdn.example.com/result.png')
  })
})
