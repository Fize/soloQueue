import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { extractImagePaths, ImageResultPreviews } from './ToolCallSegment'

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
