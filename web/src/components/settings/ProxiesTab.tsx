import { useState, useEffect } from 'react'
import { Plus, Trash2, Link as LinkIcon, Monitor, ExternalLink } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export interface ProxyInfo {
  id: string
  target_url: string
}

export function ProxiesTab() {
  const [proxies, setProxies] = useState<ProxyInfo[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [isAdding, setIsAdding] = useState(false)
  const [snackbar, setSnackbar] = useState<{ message: string; type: 'success' | 'error' } | null>(
    null
  )

  // Form state
  const [id, setId] = useState('')
  const [targetUrl, setTargetUrl] = useState('')

  const showSnackbar = (message: string, type: 'success' | 'error') => {
    setSnackbar({ message, type })
    setTimeout(() => setSnackbar(null), 4500)
  }

  const fetchProxies = async () => {
    try {
      const res = await fetch('/api/proxy')
      if (!res.ok) throw new Error('Failed to fetch proxies')
      const data = await res.json()
      setProxies(data || [])
    } catch (err: any) {
      showSnackbar('Error: ' + err.message, 'error')
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    fetchProxies()
  }, [])

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!id || !targetUrl) return
    setIsAdding(true)
    try {
      const res = await fetch('/api/proxy', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, target_url: targetUrl }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || 'Failed to add proxy')
      }
      showSnackbar(`Successfully added proxy ${id}.`, 'success')
      setId('')
      setTargetUrl('')
      fetchProxies()
      // trigger a sidebar update by firing an event
      window.dispatchEvent(new CustomEvent('proxy-updated'))
    } catch (err: any) {
      showSnackbar('Error: ' + err.message, 'error')
    } finally {
      setIsAdding(false)
    }
  }

  const handleDelete = async (deleteId: string) => {
    try {
      const res = await fetch(`/api/proxy/${deleteId}`, {
        method: 'DELETE',
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || 'Failed to delete proxy')
      }
      fetchProxies()
      // trigger a sidebar update by firing an event
      window.dispatchEvent(new CustomEvent('proxy-updated'))
    } catch (err: any) {
      toast.error(err.message || 'Failed to delete proxy')
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-4 animate-pulse">
        <div className="flex flex-col gap-1.5 mb-6">
          <div className="h-6 w-32 bg-muted rounded"></div>
          <div className="h-4 w-64 bg-muted rounded"></div>
        </div>
        <div className="h-24 w-full bg-muted rounded"></div>
        <div className="h-24 w-full bg-muted rounded"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-1.5">
        <h2 className="text-lg font-semibold tracking-tight text-foreground flex items-center gap-2">
          <Monitor className="h-5 w-5" />
          App Proxies (Iframes)
        </h2>
        <p className="text-sm text-muted-foreground">
          Configure internal applications to be proxied and embedded as iframes.
        </p>
      </div>

      {/* Add New Form */}
      <form
        onSubmit={handleAdd}
        className="flex flex-col sm:flex-row gap-3 items-end bg-muted/50 p-4 rounded-xl border border-border/50"
      >
        <div className="w-full sm:flex-1 space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground ml-1">
            ID (e.g., novel)
          </label>
          <Input
            value={id}
            onChange={(e) => setId(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
            placeholder="Unique identifier"
            className="h-9 bg-background"
          />
        </div>
        <div className="w-full sm:flex-[2] space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground ml-1">Target URL</label>
          <Input
            value={targetUrl}
            onChange={(e) => setTargetUrl(e.target.value)}
            placeholder="http://localhost:5173"
            className="h-9 bg-background"
          />
        </div>
        <Button
          type="submit"
          disabled={isAdding || !id || !targetUrl}
          className="w-full sm:w-auto h-9 gap-1.5 font-medium shadow-sm whitespace-nowrap"
        >
          <Plus className="h-4 w-4" />
          Add Proxy
        </Button>
      </form>

      {/* Proxy List */}
      <div className="grid gap-3">
        {proxies.length === 0 ? (
          <div className="text-center py-12 px-4 border border-dashed rounded-xl border-border/60 bg-muted/20">
            <Monitor className="h-8 w-8 mx-auto text-muted-foreground/50 mb-3" />
            <p className="text-sm text-muted-foreground">No proxies configured yet.</p>
          </div>
        ) : (
          proxies.map((proxy) => (
            <div
              key={proxy.id}
              className="flex flex-col sm:flex-row sm:items-center justify-between p-4 rounded-xl border bg-card shadow-sm gap-4 transition-all hover:border-primary/30 group"
            >
              <div className="flex flex-col gap-1.5 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-foreground tracking-tight">{proxy.id}</span>
                </div>
                <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <LinkIcon className="h-3 w-3 shrink-0" />
                  <span className="truncate">{proxy.target_url}</span>
                </div>
              </div>

              <div className="flex items-center gap-2 shrink-0">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 gap-1.5 text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => window.open(`/?soloqueue_proxy=${proxy.id}`, '_blank')}
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                  <span className="hidden sm:inline">Open</span>
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                  onClick={() => handleDelete(proxy.id)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            </div>
          ))
        )}
      </div>
      {/* M3 Snackbar */}
      {snackbar && (
        <div
          role="alert"
          className={`fixed bottom-20 left-1/2 -translate-x-1/2 z-[60] px-4 py-2.5 rounded-full text-sm font-medium shadow-lg animate-reveal ${
            snackbar.type === 'success'
              ? 'bg-[var(--success)] text-[var(--success-foreground)]'
              : 'bg-destructive text-destructive-foreground'
          }`}
        >
          {snackbar.message}
        </div>
      )}
    </div>
  )
}
