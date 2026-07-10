import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter'

// Map language names to dynamic imports — each loads only when first needed
const languageLoaders: Record<string, () => Promise<{ default: unknown }>> = {
  tsx: () => import('react-syntax-highlighter/dist/esm/languages/prism/tsx'),
  typescript: () => import('react-syntax-highlighter/dist/esm/languages/prism/typescript'),
  javascript: () => import('react-syntax-highlighter/dist/esm/languages/prism/javascript'),
  jsx: () => import('react-syntax-highlighter/dist/esm/languages/prism/jsx'),
  python: () => import('react-syntax-highlighter/dist/esm/languages/prism/python'),
  rust: () => import('react-syntax-highlighter/dist/esm/languages/prism/rust'),
  go: () => import('react-syntax-highlighter/dist/esm/languages/prism/go'),
  c: () => import('react-syntax-highlighter/dist/esm/languages/prism/c'),
  cpp: () => import('react-syntax-highlighter/dist/esm/languages/prism/cpp'),
  java: () => import('react-syntax-highlighter/dist/esm/languages/prism/java'),
  json: () => import('react-syntax-highlighter/dist/esm/languages/prism/json'),
  yaml: () => import('react-syntax-highlighter/dist/esm/languages/prism/yaml'),
  css: () => import('react-syntax-highlighter/dist/esm/languages/prism/css'),
  markup: () => import('react-syntax-highlighter/dist/esm/languages/prism/markup'),
  bash: () => import('react-syntax-highlighter/dist/esm/languages/prism/bash'),
  sql: () => import('react-syntax-highlighter/dist/esm/languages/prism/sql'),
  toml: () => import('react-syntax-highlighter/dist/esm/languages/prism/toml'),
  docker: () => import('react-syntax-highlighter/dist/esm/languages/prism/docker'),
  graphql: () => import('react-syntax-highlighter/dist/esm/languages/prism/graphql'),
  protobuf: () => import('react-syntax-highlighter/dist/esm/languages/prism/protobuf'),
}

const loadedLanguages = new Set<string>()
const pendingLoads = new Map<string, Promise<void>>()

/** Check if a language grammar has already been registered. */
export function isLanguageLoaded(lang: string): boolean {
  return loadedLanguages.has(lang)
}

/**
 * Ensure a language grammar is registered with the SyntaxHighlighter.
 * Returns immediately if already loaded; otherwise triggers a dynamic import.
 * Safe to call multiple times for the same language — deduplicates.
 */
export async function ensureLanguage(lang: string): Promise<void> {
  if (!lang) return
  const normalized = lang.toLowerCase()
  if (loadedLanguages.has(normalized)) return

  const loader = languageLoaders[normalized]
  if (!loader) return // Unsupported language — will render as plain text

  // Deduplicate concurrent loads for the same language
  const pending = pendingLoads.get(normalized)
  if (pending) return pending

  const loadPromise = loader()
    .then((mod) => {
      SyntaxHighlighter.registerLanguage(normalized, mod.default)
      loadedLanguages.add(normalized)
    })
    .finally(() => {
      pendingLoads.delete(normalized)
    })

  pendingLoads.set(normalized, loadPromise)
  return loadPromise
}

/**
 * Eagerly register the most common languages so they're instantly available
 * for the first chat message. These are bundled in the initial chunk.
 */
export function preloadCommonLanguages(): void {
  const common = ['typescript', 'javascript', 'python', 'bash', 'json', 'tsx']
  for (const lang of common) {
    ensureLanguage(lang)
  }
}
