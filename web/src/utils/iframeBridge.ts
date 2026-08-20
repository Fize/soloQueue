/**
 * Injects a script and style into an HTML string to enable visual annotation
 * (hover, click, draw) for iframe previews.
 */
import bridgeScript from '@/assets/selection-bridge.js?raw'

export function injectSelectionBridge(doc: string): string {
  // Prevent double injection
  if (doc.includes('data-od-selection-bridge')) return doc

  const script = `<script data-od-selection-bridge>${bridgeScript}</script>`

  const style = `<style data-od-selection-bridge-style>
html[data-od-comment-mode] body * { cursor: crosshair !important; }
html[data-od-comment-mode][data-od-comment-mode-kind="pod"] body * { cursor: cell !important; }
html[data-od-comment-mode] body iframe { pointer-events: none !important; }
</style>`

  return injectBeforeBodyEnd(injectBeforeHeadEnd(doc, style), script)
}

function injectBeforeHeadEnd(doc: string, injection: string): string {
  const i = doc.indexOf('</head>')
  if (i >= 0) return doc.slice(0, i) + injection + '\n' + doc.slice(i)
  const j = doc.indexOf('<body')
  if (j >= 0) return doc.slice(0, j) + injection + '\n' + doc.slice(j)
  return injection + '\n' + doc
}

function injectBeforeBodyEnd(doc: string, injection: string): string {
  const i = doc.lastIndexOf('</body>')
  if (i >= 0) return doc.slice(0, i) + injection + '\n' + doc.slice(i)
  const j = doc.lastIndexOf('</html>')
  if (j >= 0) return doc.slice(0, j) + injection + '\n' + doc.slice(j)
  return doc + '\n' + injection
}
