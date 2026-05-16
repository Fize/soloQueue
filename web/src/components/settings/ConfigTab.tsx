import { useState, useEffect } from 'react'
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'
import toml from 'react-syntax-highlighter/dist/esm/languages/prism/toml'
import { getConfigToml } from '@/lib/api'
import { FileText, AlertCircle } from 'lucide-react'

SyntaxHighlighter.registerLanguage('toml', toml)

export function ConfigTab() {
  const [content, setContent] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    load()
  }, [])

  const load = async () => {
    setLoading(true)
    setError(null)
    try {
      const raw = await getConfigToml()
      setContent(raw)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return <div className="text-sm text-muted-foreground">Loading settings.toml...</div>
  }

  if (error) {
    return (
      <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
        <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
        <div>
          <p className="font-medium">Failed to load settings.toml</p>
          <p className="mt-1 text-destructive/80">{error}</p>
        </div>
      </div>
    )
  }

  if (!content) {
    return <div className="text-sm text-muted-foreground">No content.</div>
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <FileText className="h-4 w-4" />
        <span>~/.soloqueue/settings.toml</span>
      </div>
      <div className="overflow-hidden rounded-lg border bg-white">
        <SyntaxHighlighter
          language="toml"
          style={oneLight}
          customStyle={{
            margin: 0,
            borderRadius: 0,
            fontSize: '13px',
            lineHeight: '1.6',
          }}
          showLineNumbers
        >
          {content}
        </SyntaxHighlighter>
      </div>
    </div>
  )
}
