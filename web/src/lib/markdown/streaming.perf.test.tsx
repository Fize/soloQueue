// Per-delta cost of rendering a growing assistant message.
//
// The baseline (`MarkdownPreview` over the whole string) is what the chat did
// for every token; `LongMarkdown` freezes completed blocks and re-parses only
// the tail. jsdom has no layout, so this measures the markdown pipeline plus the
// React commit — the layout half is verified by hand in DevTools.
//
// Run with `pnpm bench` (it is also part of `pnpm test`).
import { afterAll, describe, expect, it } from 'vitest'
import { flushSync } from 'react-dom'
import { createRoot } from 'react-dom/client'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { LongMarkdown } from '@/components/ui/long-markdown'

const DELTA = 'The quick brown fox jumps over the lazy dog while the storage policy adapter decides. '

function documentOf(size: number): string {
  let body = 'Options:\n\n'
  let index = 1
  while (body.length < size) {
    body += `${index}. ${DELTA}${DELTA}\n\n`
    if (index % 7 === 0) body += '- first bullet in a list\n- second bullet with more detail\n\n'
    index++
  }
  return body
}

const container = document.createElement('div')
document.body.appendChild(container)
const root = createRoot(container)

/** Median milliseconds per appended delta. */
function measure(base: string, samples: number, render: (content: string) => void): number {
  let text = base
  const timings: number[] = []
  for (let sample = 0; sample < samples; sample++) {
    // Each sample is one token-sized append onto the already rendered answer.
    text = text.length > base.length + 4_000 ? base : text + DELTA
    const started = performance.now()
    flushSync(() => render(text))
    timings.push(performance.now() - started)
  }
  timings.sort((a, b) => a - b)
  return timings[Math.floor(timings.length / 2)]
}

const rows: Array<{ size: string; baseline: number; chunked: number }> = []

afterAll(() => {
  const lines = rows.map(
    (row) =>
      `${row.size.padStart(6)}  MarkdownPreview ${row.baseline.toFixed(2).padStart(7)} ms` +
      `  LongMarkdown ${row.chunked.toFixed(2).padStart(6)} ms`
  )
  process.stdout.write(`\nper-delta markdown render (median)\n${lines.join('\n')}\n\n`)
})

describe('streaming markdown per delta', () => {
  for (const size of [2_000, 20_000, 100_000]) {
    it(`keeps a ${Math.round(size / 1024)} KB answer cheap to update`, () => {
      const base = documentOf(size)
      const samples = size >= 100_000 ? 5 : 9
      const baseline = measure(base, samples, (content) =>
        root.render(<MarkdownPreview content={content} isAnimating />)
      )
      const chunked = measure(base, samples, (content) =>
        root.render(<LongMarkdown content={content} isAnimating />)
      )
      rows.push({ size: `${Math.round(size / 1024)} KB`, baseline, chunked })

      // A long answer must not cost more per token than a short one. The bound
      // is deliberately loose: this guards against reintroducing a full
      // re-parse, not against millisecond noise.
      if (size >= 20_000) expect(chunked).toBeLessThan(baseline * 0.8)
    })
  }
})
