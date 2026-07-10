import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock SyntaxHighlighter
vi.mock('react-syntax-highlighter', () => ({
  PrismLight: { registerLanguage: vi.fn() },
}))

// Mock ALL language imports so dynamic import() returns something
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/tsx', () => ({
  default: { grammar: 'tsx' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/typescript', () => ({
  default: { grammar: 'typescript' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/javascript', () => ({
  default: { grammar: 'javascript' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/jsx', () => ({
  default: { grammar: 'jsx' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/python', () => ({
  default: { grammar: 'python' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/rust', () => ({
  default: { grammar: 'rust' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/go', () => ({
  default: { grammar: 'go' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/c', () => ({
  default: { grammar: 'c' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/cpp', () => ({
  default: { grammar: 'cpp' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/java', () => ({
  default: { grammar: 'java' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/json', () => ({
  default: { grammar: 'json' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/yaml', () => ({
  default: { grammar: 'yaml' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/css', () => ({
  default: { grammar: 'css' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/markup', () => ({
  default: { grammar: 'markup' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/bash', () => ({
  default: { grammar: 'bash' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/sql', () => ({
  default: { grammar: 'sql' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/toml', () => ({
  default: { grammar: 'toml' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/docker', () => ({
  default: { grammar: 'docker' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/graphql', () => ({
  default: { grammar: 'graphql' },
}))
vi.mock('react-syntax-highlighter/dist/esm/languages/prism/protobuf', () => ({
  default: { grammar: 'protobuf' },
}))

import { ensureLanguage, isLanguageLoaded, preloadCommonLanguages } from './syntax-languages'
import { PrismLight } from 'react-syntax-highlighter'

describe('syntax-languages', () => {
  describe('ensureLanguage', () => {
    it('returns immediately for empty/null input', async () => {
      await ensureLanguage('')
      expect(PrismLight.registerLanguage).not.toHaveBeenCalled()
    })

    it('returns immediately for unsupported language', async () => {
      await ensureLanguage('brainfuck')
      expect(PrismLight.registerLanguage).not.toHaveBeenCalled()
    })

    it('loads a supported language and registers it', async () => {
      await ensureLanguage('bash')
      expect(PrismLight.registerLanguage).toHaveBeenCalledWith('bash', {
        grammar: 'bash',
      })
    })

    it('marks language as loaded after registration', async () => {
      await ensureLanguage('python')
      expect(isLanguageLoaded('python')).toBe(true)
    })

    it('is case-insensitive', async () => {
      await ensureLanguage('JavaScript')
      expect(isLanguageLoaded('javascript')).toBe(true)
    })

    it('does not reload an already loaded language', async () => {
      vi.clearAllMocks()
      await ensureLanguage('go')
      expect(PrismLight.registerLanguage).toHaveBeenCalledTimes(1)
      vi.clearAllMocks()
      await ensureLanguage('go')
      expect(PrismLight.registerLanguage).not.toHaveBeenCalled()
    })
  })

  describe('preloadCommonLanguages', () => {
    it('starts loading all common languages', async () => {
      preloadCommonLanguages()
      // Wait for all pending loads
      await vi.waitFor(() => {
        expect(isLanguageLoaded('typescript')).toBe(true)
        expect(isLanguageLoaded('javascript')).toBe(true)
        expect(isLanguageLoaded('python')).toBe(true)
        expect(isLanguageLoaded('bash')).toBe(true)
        expect(isLanguageLoaded('json')).toBe(true)
        expect(isLanguageLoaded('tsx')).toBe(true)
      })
    })
  })
})
