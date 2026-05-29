import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { MonitorX, Loader2 } from 'lucide-react'

interface ProxyInfo {
  id: string
  target_url: string
}

export function IframePageView() {
  const { id } = useParams()
  const [proxy, setProxy] = useState<ProxyInfo | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const fetchProxy = async () => {
      try {
        setIsLoading(true)
        const res = await fetch('/api/proxy')
        if (!res.ok) throw new Error('Failed to fetch proxy configs')
        const proxies: ProxyInfo[] = await res.json()
        const found = proxies.find((p) => p.id === id)
        if (found) {
          setProxy(found)
        } else {
          setError(`Proxy configuration for "${id}" not found.`)
        }
      } catch (err: any) {
        setError(err.message || 'Failed to load iframe')
      } finally {
        setIsLoading(false)
      }
    }

    if (id) {
      fetchProxy()
    }
  }, [id])

  if (isLoading) {
    return (
      <div className="flex h-full w-full items-center justify-center bg-background/50 backdrop-blur-sm">
        <div className="flex flex-col items-center gap-4 text-muted-foreground">
          <Loader2 className="h-10 w-10 animate-spin opacity-50" />
          <div className="space-y-2 text-center">
            <div className="font-semibold text-sm">Loading iframe...</div>
          </div>
        </div>
      </div>
    )
  }

  if (error || !proxy) {
    return (
      <div className="flex h-full w-full flex-col items-center justify-center text-muted-foreground p-8 text-center bg-muted/10">
        <MonitorX className="h-12 w-12 mb-4 opacity-50" />
        <h3 className="text-lg font-semibold text-foreground mb-1">Iframe Not Available</h3>
        <p className="text-sm max-w-md">{error || 'Unknown error occurred.'}</p>
      </div>
    )
  }

  const srcUrl = `/?soloqueue_proxy=${proxy.id}`

  return (
    <div className="flex h-full w-full flex-col overflow-hidden bg-background">
      <iframe
        src={srcUrl}
        className="flex-1 w-full h-full border-0"
        title={`Proxy - ${proxy.id}`}
        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
        allowFullScreen
      />
    </div>
  )
}
