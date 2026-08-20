import { beforeEach, describe, expect, it } from 'vitest'
import { applyTheme } from './theme'

describe('theme classes', () => {
  beforeEach(() => {
    document.documentElement.className = ''
  })

  it('marks dark mode with dark and removes light', () => {
    applyTheme('dark')

    expect(document.documentElement).toHaveClass('dark')
    expect(document.documentElement).not.toHaveClass('light')
  })

  it('marks light mode with light and removes dark', () => {
    applyTheme('light')

    expect(document.documentElement).toHaveClass('light')
    expect(document.documentElement).not.toHaveClass('dark')
  })
})
