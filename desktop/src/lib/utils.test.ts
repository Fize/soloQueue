import { describe, it, expect } from 'vitest'
import { cn, pathsMatch } from './utils'

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

describe('pathsMatch', () => {
  it('matches a configured tilde path with a restored absolute macOS path', () => {
    expect(pathsMatch('~/github.com/soloQueue/', '/Users/xiaobaitu/github.com/soloQueue')).toBe(true)
  })

  it('matches a configured tilde path with a restored absolute Linux path', () => {
    expect(pathsMatch('~/projects/soloQueue', '/home/developer/projects/soloQueue/')).toBe(true)
  })

  it('does not match local with a project path or different projects', () => {
    expect(pathsMatch('', '/Users/xiaobaitu/github.com/soloQueue')).toBe(false)
    expect(pathsMatch('~/projects/one', '~/projects/two')).toBe(false)
  })
})
