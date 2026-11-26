import { describe, expect, it } from 'vitest'
import { CHUNK_BUDGET, CHUNK_HARD_CAP, splitChunks } from './chunks'

/** Every fence opened inside `chunk` must be closed inside it. */
function fencesBalanced(chunk: string): boolean {
  let open: string | null = null
  for (const line of chunk.split('\n')) {
    const match = /^\s{0,3}(`{3,}|~{3,})/.exec(line)
    if (!match) continue
    const mark = match[1][0]
    if (open === null) open = mark
    else if (mark === open && line.trimStart().slice(match[1].length).trim() === '') open = null
  }
  return open === null
}

const paragraph = (index: number) =>
  `Paragraph ${index}. ${'The quick brown fox jumps over the lazy dog. '.repeat(5)}\n\n`

/** Whole blocks only, so every fence in the fixture is closed. */
function markdown(size: number): string {
  let body = 'Options:\n\n'
  let index = 1
  while (body.length < size) {
    if (index % 5 === 0) {
      body +=
        '```go\n' + 'type FilePolicy struct { MaxFileMB, RetentionDays, MaxFiles int }\n'.repeat(3) + '```\n\n'
    }
    body += paragraph(index++)
  }
  return body
}

function expectLossless(text: string) {
  const { chunks, growing } = splitChunks(text)
  expect([...chunks, growing].join('')).toBe(text)
  expect(chunks.every((chunk) => chunk.length > 0)).toBe(true)
  return { chunks, growing }
}

describe('splitChunks', () => {
  it('keeps short documents in the growing tail', () => {
    const text = markdown(1_000)
    const { chunks, growing } = expectLossless(text)
    expect(chunks).toEqual([])
    expect(growing).toBe(text)
  })

  it('cuts long documents at blank lines and bounds the tail', () => {
    const text = markdown(120_000)
    const { chunks, growing } = expectLossless(text)
    expect(chunks.length).toBeGreaterThan(10)
    expect(growing.length).toBeLessThanOrEqual(CHUNK_BUDGET)
  })

  it('never cuts inside a closed code fence', () => {
    const code = '```ts\n' + 'const value = compute(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)\n'.repeat(600) + '```\n\n'
    const text = markdown(20_000) + code + markdown(20_000)
    const { chunks } = expectLossless(text)
    expect(chunks.length).toBeGreaterThan(1)
    for (const chunk of chunks) expect(fencesBalanced(chunk)).toBe(true)
    expect(chunks.join('')).toContain(code)
  })

  it('keeps an unclosed trailing fence whole inside the growing tail', () => {
    const text = markdown(20_000) + '```ts\nconst value = compute(1, 2, 3, 4, 5)\n'
    const { chunks, growing } = expectLossless(text)
    for (const chunk of chunks) expect(fencesBalanced(chunk)).toBe(true)
    expect(growing).toContain('```ts\nconst value = compute(1, 2, 3, 4, 5)\n')
  })

  it('emits an oversized fence whole instead of blocking later cuts', () => {
    const code = '```ts\n' + 'const value = compute(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)\n'.repeat(600) + '```\n\n'
    const text = markdown(4_000) + code + markdown(40_000)
    const { chunks, growing } = expectLossless(text)
    expect(chunks.join('')).toContain(code)
    expect(chunks.length).toBeGreaterThan(2)
    expect(growing.length).toBeLessThanOrEqual(CHUNK_BUDGET)
    for (const chunk of chunks) expect(fencesBalanced(chunk)).toBe(true)
  })

  it('does not split a list across blank lines', () => {
    const list = Array.from({ length: 80 }, (_, i) => `${i + 1}. step number ${i + 1} with some detail text`).join('\n\n')
    const text = `${'intro paragraph\n\n'.repeat(400)}${list}\n\nafter\n\n`
    const { chunks, growing } = expectLossless(text)
    expect(chunks.length).toBeGreaterThan(0)
    expect([...chunks, growing].join('')).toContain(list)
  })

  it('keeps earlier chunks identical while the document grows', () => {
    const text = markdown(60_000)
    const first = splitChunks(text)
    const grown = splitChunks(text + markdown(10_000))
    expect(grown.chunks.length).toBeGreaterThan(first.chunks.length)
    expect(grown.chunks.slice(0, first.chunks.length)).toEqual(first.chunks)
  })

  it('cuts a single-line payload that has no blank lines', () => {
    const text = '{"status":200,"body":"' + 'x'.repeat(CHUNK_HARD_CAP * 3) + '"}'
    const { chunks, growing } = expectLossless(text)
    expect(chunks.length).toBe(3)
    expect(growing.length).toBeLessThanOrEqual(CHUNK_HARD_CAP)
  })

  it('does not hard-cut an unbreakable run that still fits the cap', () => {
    const text = 'x'.repeat(CHUNK_HARD_CAP / 2) + '\n\n' + 'y'.repeat(20_000)
    const { chunks, growing } = expectLossless(text)
    expect(chunks.length).toBe(1)
    expect(chunks[0].startsWith('x')).toBe(true)
    expect(growing.startsWith('y')).toBe(true)
  })

  it('cuts a long run at a line boundary instead of mid-line', () => {
    const run = Array.from({ length: 8_000 }, (_, i) => `line ${i}`).join('\n')
    const { chunks } = expectLossless(run)
    expect(chunks.length).toBeGreaterThan(0)
    for (const chunk of chunks) expect(chunk.endsWith('\n')).toBe(true)
  })
})
