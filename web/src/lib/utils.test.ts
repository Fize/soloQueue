import { describe, it, expect } from 'vitest'
import { cn, formatToolCallHeader, getToolCallSummary, getToolSource, pathsMatch } from './utils'

describe('cn', () => {
  it('merges class names', () => {
    expect(cn('a', 'b')).toBe('a b')
  })

  it('handles conditional classes', () => {
    const showHidden = false
    expect(cn('base', showHidden && 'hidden', 'visible')).toBe('base visible')
  })

  it('merges tailwind conflicts', () => {
    expect(cn('px-4', 'px-2')).toBe('px-2')
  })

  it('handles undefined/null', () => {
    expect(cn('a', undefined, null, 'b')).toBe('a b')
  })
})

describe('getToolCallSummary', () => {
  it('uses the unified ImageTool prompt', () => {
    expect(getToolCallSummary('ImageTool', '{"action":"edit","prompt":"make it blue"}')).toBe(
      'make it blue'
    )
  })

  it('does not add special handling to other image-like tools', () => {
    expect(getToolCallSummary('LegacyImageTool', '{"images":["source.png"]}')).toBe('')
  })
})

describe('getToolSource', () => {
  it('classifies lsp, mcp, skill and builtin tools', () => {
    expect(getToolSource('lsp__hover')).toBe('lsp')
    expect(getToolSource('mcp__playwright__browser_click')).toBe('mcp')
    expect(getToolSource('Skill')).toBe('skill')
    expect(getToolSource('Bash')).toBe('builtin')
  })

  it('does not treat look-alike names as a source prefix', () => {
    expect(getToolSource('lsp')).toBe('builtin')
    expect(getToolSource('mcp')).toBe('builtin')
    expect(getToolSource('skills')).toBe('builtin')
  })
})

describe('formatToolCallHeader', () => {
  it('renders mcp tools as server/tool without emoji', () => {
    expect(formatToolCallHeader('mcp__playwright__browser_click', '{}')).toBe(
      'playwright/browser_click'
    )
  })

  it('keeps every multi-segment mcp tool name', () => {
    expect(formatToolCallHeader('mcp__a__b__c', '{}')).toBe('a/b__c')
  })

  it('renders lsp actions without prefix or emoji', () => {
    expect(formatToolCallHeader('lsp__hover', '{"file":"/repo/src/utils.ts"}')).toBe(
      'hover   utils.ts'
    )
  })

  it('renders skill calls as summary only', () => {
    expect(formatToolCallHeader('Skill', '{"skill":"qa-suite"}')).toBe('qa-suite')
    expect(formatToolCallHeader('Skill', '{"skill":"qa-suite","args":"web"}')).toBe(
      'qa-suite (web)'
    )
  })

  it('leaves builtin tool headers unchanged', () => {
    expect(formatToolCallHeader('Bash', '{"command":"pnpm test"}')).toBe('Bash   pnpm test')
  })
})

describe('pathsMatch', () => {
  it('matches a configured tilde path with a restored absolute macOS path', () => {
    expect(pathsMatch('~/github.com/soloQueue/', '/Users/xiaobaitu/github.com/soloQueue')).toBe(
      true
    )
  })

  it('matches a configured tilde path with a restored absolute Linux path', () => {
    expect(pathsMatch('~/projects/soloQueue', '/home/developer/projects/soloQueue/')).toBe(true)
  })

  it('does not match local with a project path or different projects', () => {
    expect(pathsMatch('', '/Users/xiaobaitu/github.com/soloQueue')).toBe(false)
    expect(pathsMatch('~/projects/one', '~/projects/two')).toBe(false)
  })
})
