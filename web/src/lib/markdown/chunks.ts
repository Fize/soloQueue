// Splits a growing markdown document into immutable chunks plus one growing
// tail. Streamed output appends characters, so re-rendering the whole document
// on every delta makes the per-delta cost grow with the answer length — at
// 100 KB the markdown pipeline alone costs tens of milliseconds per token.
//
// Emitted chunks are append-only: once a chunk is returned it can never change,
// which lets the renderer memoize it and parse each byte of the document once.
// Only the tail (`growing`) is re-parsed per delta and it stays near the budget.
//
// Cut points are blank lines outside code fences that do not split a list or an
// indented code block, so both sides of a cut are complete markdown blocks.

/** Characters kept in the growing tail that is re-rendered per delta. */
export const CHUNK_BUDGET = 8 * 1024
/** Smallest emitted chunk; avoids many tiny chunks for dense blank-line text. */
export const CHUNK_MIN = 4 * 1024
/**
 * Cut inside an unbreakable run (a paragraph with no blank line) once it grows
 * past this size. A line boundary inside a paragraph renders as a soft break,
 * which is visually identical, so this only trips on pathologically long runs.
 */
export const CHUNK_HARD_CAP = 32 * 1024

export interface ChunkedText {
  /** Immutable complete blocks, in order. */
  chunks: string[]
  /** The tail that is still growing; empty once the document ends on a block. */
  growing: string
}

const FENCE_RE = /^\s{0,3}(`{3,}|~{3,})/
const LIST_RE = /^\s*(?:[-*+]|\d{1,9}[.)])\s+/
const INDENTED_CODE_RE = /^(?: {4}|\t)/

interface Scan {
  /** Index right before a line that follows a blank line, fence-safe. */
  boundaries: number[]
  /** Index right after every newline outside a fence. */
  safeNewlines: number[]
  /** True when no fence opens at or after `from`. */
  hasNoFenceAfter: (from: number) => boolean
}

function scan(text: string): Scan {
  const boundaries: number[] = []
  const safeNewlines: number[] = []
  const fenceMarkers: number[] = []
  let fence: '`' | '~' | null = null
  let previousMeaningful = ''
  let blankStart = -1
  let offset = 0

  while (offset <= text.length) {
    const newline = text.indexOf('\n', offset)
    const lineEnd = newline === -1 ? text.length : newline
    const line = text.slice(offset, lineEnd)
    const trimmed = line.trim()
    const lineStart = offset
    const next = newline === -1 ? text.length + 1 : newline + 1

    const fenceMatch = FENCE_RE.exec(line)
    const startsWith = line.trimStart()
    if (fence === null && fenceMatch) {
      fenceMarkers.push(lineStart)
      fence = fenceMatch[1][0] as '`' | '~'
      // The blank line before a fence must not become a cut at its closing line.
      blankStart = -1
      previousMeaningful = line
    } else if (
      fence !== null &&
      fenceMatch &&
      fenceMatch[1][0] === fence &&
      startsWith.slice(fenceMatch[1].length).trim() === ''
    ) {
      // Only a bare marker closes a fence; an info string would open a new one.
      fence = null
    }

    if (fence === null) {
      if (newline !== -1) safeNewlines.push(next)
      if (trimmed === '') {
        if (blankStart < 0) blankStart = lineStart
      } else if (blankStart >= 0) {
        // A blank line separates two non-blank lines: the following line starts
        // a new block unless it continues a list or an indented code block.
        const continuesList = LIST_RE.test(previousMeaningful) && LIST_RE.test(line)
        const continuesCode = INDENTED_CODE_RE.test(previousMeaningful) && INDENTED_CODE_RE.test(line)
        if (!continuesList && !continuesCode) boundaries.push(lineStart)
        blankStart = -1
        previousMeaningful = line
      } else {
        previousMeaningful = line
      }
    }

    if (newline === -1) break
    offset = next
  }

  return {
    boundaries,
    safeNewlines,
    hasNoFenceAfter: (start) => !fenceMarkers.some((marker) => marker >= start),
  }
}

export function splitChunks(text: string, budget: number = CHUNK_BUDGET): ChunkedText {
  if (!text) return { chunks: [], growing: '' }
  if (text.length <= budget) return { chunks: [], growing: text }

  const { boundaries, safeNewlines, hasNoFenceAfter } = scan(text)
  const chunks: string[] = []
  let start = 0
  let cursor = 0

  while (text.length - start > budget) {
    const minimum = start + Math.min(CHUNK_MIN, budget)
    while (cursor < boundaries.length && boundaries[cursor] < minimum) cursor++
    let cut = cursor < boundaries.length ? boundaries[cursor] : -1

    if (cut === -1 || cut - start > CHUNK_HARD_CAP) {
      cut = hardCut(text, start, safeNewlines, hasNoFenceAfter)
    }
    if (cut <= start) break
    chunks.push(text.slice(start, cut))
    start = cut
  }

  return { chunks, growing: text.slice(start) }
}

/**
 * Last resort for a run with no blank line, in order of preference:
 * 1. the furthest line boundary inside the hard cap;
 * 2. the end of a code fence that is larger than the cap, so an oversized
 *    fence becomes one complete chunk instead of blocking every later cut;
 * 3. an exact-length cut when the run holds no fence at all (a single-line
 *    payload, where a line boundary does not exist).
 */
function hardCut(
  text: string,
  start: number,
  safeNewlines: number[],
  hasNoFenceAfter: (from: number) => boolean
): number {
  const limit = start + CHUNK_HARD_CAP
  let cut = -1
  for (const candidate of safeNewlines) {
    if (candidate <= start) continue
    if (candidate > limit) break
    cut = candidate
  }
  if (cut > start) return cut
  if (!hasNoFenceAfter(start)) {
    // The run opens a fence. Cutting inside it would render two broken code
    // blocks, so emit it whole once it closes and leave it in the tail until then.
    const fenceEnd = safeNewlines.find((candidate) => candidate > start)
    return fenceEnd ?? -1
  }
  if (text.length < limit) return -1
  return limit
}
