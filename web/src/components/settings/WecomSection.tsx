import { useCallback, useEffect, useRef, useState } from 'react'
import { MessageCircle, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { deleteWecomBotConfig, getWecomBotsConfig, updateWecomBotsConfig } from '@/lib/api'
import type { WecomBotConfig } from '@/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const stateLabels: Record<string, string> = {
  disabled: 'Disabled', disconnected: 'Disconnected', connecting: 'Connecting',
  connected: 'Connected', reconnecting: 'Reconnecting', auth_failed: 'Authentication failed', conflict: 'Connection replaced',
}

function BotDialog({ account, accounts, onClose, onSaved }: {
  account?: WecomBotConfig
  accounts: WecomBotConfig[]
  onClose: () => void
  onSaved: (bots: WecomBotConfig[]) => void
}) {
  const [name, setName] = useState(account?.name || '')
  const [botId, setBotId] = useState(account?.botId || '')
  const [secret, setSecret] = useState('')
  const [whitelistEnabled, setWhitelistEnabled] = useState(account?.whitelist_enabled || false)
  const [whitelist, setWhitelist] = useState((account?.whitelist || []).join('\n'))
  const [saving, setSaving] = useState(false)
  const savingRef = useRef(false)
  const [error, setError] = useState('')
  const save = async () => {
    if (savingRef.current) return
    if (!name.trim() || !botId.trim() || ((!account?.credentialConfigured || botId.trim() !== account.botId) && !secret.trim())) {
      setError('Enter a name, Bot ID and Secret. Changing Bot ID requires a new Secret.')
      return
    }
    savingRef.current = true
    setSaving(true)
    setError('')
    try {
      const bot: WecomBotConfig & { secret?: string } = {
        ...(account || { id: '', enabled: true, credentialConfigured: false, connected: false, state: 'disconnected', bind_type: 'l1' as const }),
        name: name.trim(), botId: botId.trim(), whitelist_enabled: whitelistEnabled,
        whitelist: whitelist.split(/[\s,]+/).filter(Boolean),
        ...(secret.trim() ? { secret: secret.trim() } : {}),
      }
      const next = account ? accounts.map(item => item.id === account.id ? bot : item) : [...accounts, bot]
      onSaved(await updateWecomBotsConfig(next))
      onClose()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to save WeCom bot')
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }
  return <Dialog open onOpenChange={open => { if (!open && !saving) onClose() }}>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{account ? 'Edit WeCom bot' : 'Add WeCom bot'}</DialogTitle>
        <DialogDescription>Create an enterprise WeChat intelligent bot in API mode and choose long connection. Bind it in L1 or team agent settings.</DialogDescription>
      </DialogHeader>
      <div className="space-y-3">
        <div className="space-y-1"><Label htmlFor="wecom-name">Name</Label><Input id="wecom-name" value={name} onChange={e => setName(e.target.value)} disabled={saving} autoFocus /></div>
        <div className="space-y-1"><Label htmlFor="wecom-bot-id">Bot ID</Label><Input id="wecom-bot-id" value={botId} onChange={e => setBotId(e.target.value)} disabled={saving} /></div>
        <div className="space-y-1"><Label htmlFor="wecom-secret">Secret</Label><Input id="wecom-secret" type="password" autoComplete="new-password" value={secret} onChange={e => setSecret(e.target.value)} disabled={saving} placeholder={account?.credentialConfigured ? 'Leave empty to keep the current Secret' : ''} /></div>
        <div className="flex items-center justify-between"><Label htmlFor="wecom-whitelist">Restrict to allowed users</Label><Switch id="wecom-whitelist" checked={whitelistEnabled} onCheckedChange={setWhitelistEnabled} disabled={saving} /></div>
        {whitelistEnabled && <div className="space-y-1"><Label htmlFor="wecom-users">Allowed user IDs</Label><Input id="wecom-users" value={whitelist} onChange={e => setWhitelist(e.target.value)} placeholder="User IDs separated by spaces or commas" disabled={saving} /></div>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter><Button variant="outline" disabled={saving} onClick={onClose}>Cancel</Button><Button disabled={saving} onClick={() => { void save() }}>{saving ? 'Saving…' : account ? 'Save changes' : 'Add WeCom bot'}</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}

export function WecomSection() {
  const [accounts, setAccounts] = useState<WecomBotConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<WecomBotConfig>()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [removing, setRemoving] = useState<WecomBotConfig>()
  const [saving, setSaving] = useState(false)
  const busy = useRef(false)
  const revision = useRef(0)
  const removingRef = useRef(false)
  const load = useCallback(async () => {
    if (busy.current) return
    const version = revision.current
    try { const next = await getWecomBotsConfig(); if (!busy.current && version === revision.current) { setAccounts(next); setError('') } }
    catch (cause) { if (!busy.current && version === revision.current) setError(cause instanceof Error ? cause.message : 'Failed to load WeCom bots') }
    finally { setLoading(false) }
  }, [])
  useEffect(() => {
    const initial = window.setTimeout(() => { void load() }, 0)
    const timer = window.setInterval(() => { void load() }, 5000)
    return () => { window.clearTimeout(initial); window.clearInterval(timer) }
  }, [load])
  const toggle = async (bot: WecomBotConfig, enabled: boolean) => {
    if (busy.current) return
    busy.current = true; revision.current++; setSaving(true)
    const previous = accounts
    const next = accounts.map(item => item.id === bot.id ? { ...item, enabled } : item)
    setAccounts(next)
    try { setAccounts(await updateWecomBotsConfig(next)) }
    catch (cause) { setAccounts(previous); toast.error(cause instanceof Error ? cause.message : 'Failed to update WeCom bot') }
    finally { busy.current = false; setSaving(false) }
  }
  const remove = async () => {
    if (!removing || removingRef.current) return
    removingRef.current = true
    busy.current = true; revision.current++; setSaving(true)
    try { await deleteWecomBotConfig(removing.id); setAccounts(items => items.filter(item => item.id !== removing.id)); setRemoving(undefined) }
    catch (cause) { toast.error(cause instanceof Error ? cause.message : 'Failed to remove WeCom bot') }
    finally { busy.current = false; removingRef.current = false; setSaving(false) }
  }
  return <section aria-label="WeCom" className="space-y-4 rounded-xl border border-border bg-card p-6 shadow-sm">
    <div className="flex items-center justify-between gap-4 border-b border-border pb-3">
      <div><div className="flex items-center gap-2"><MessageCircle className="size-4 text-primary" /><h2 className="font-semibold">WeCom · 企业微信</h2></div><p className="mt-1 text-xs text-muted-foreground">Intelligent bots using API long connection. Text messages are supported.</p></div>
      <div className="flex gap-2"><Button variant="outline" size="sm" aria-label="Refresh WeCom status" disabled={saving || dialogOpen || !!removing} onClick={() => { void load() }}><RefreshCw /></Button><Button size="sm" disabled={saving || loading || !!error || dialogOpen || !!removing} onClick={() => { setEditing(undefined); setDialogOpen(true); busy.current = true; revision.current++ }}><Plus />Add WeCom bot</Button></div>
    </div>
    {loading ? <p className="text-sm text-muted-foreground">Loading WeCom bots…</p> : error ? <p role="alert" className="text-sm text-destructive">{error}</p> : accounts.length === 0 ? <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">No WeCom bots configured</p> : <div className="space-y-3">{accounts.map(bot => <div key={bot.id} className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/20 p-4">
      <div className="min-w-0"><div className="truncate font-medium">{bot.name}</div><p className="mt-1 text-xs text-muted-foreground">{bot.botId} · {stateLabels[bot.state] || bot.state} · {bot.bind_type === 'l2' ? `L2 · ${bot.bind_agent}` : 'L1'}</p>{bot.error && <p className="text-xs text-destructive">{bot.error}</p>}</div>
      <div className="flex items-center gap-2"><label className="sr-only" htmlFor={`wecom-enabled-${bot.id}`}>Enable WeCom {bot.name}</label><Switch id={`wecom-enabled-${bot.id}`} checked={bot.enabled} disabled={saving || dialogOpen || !!removing} onCheckedChange={enabled => { void toggle(bot, enabled) }} /><Button size="xs" variant="outline" disabled={saving || dialogOpen || !!removing} aria-label={`Edit WeCom ${bot.name}`} onClick={() => { setEditing(bot); setDialogOpen(true); busy.current = true; revision.current++ }}><Pencil />Edit</Button><Button size="icon-xs" variant="ghost" disabled={saving || dialogOpen || !!removing} aria-label={`Remove WeCom ${bot.name}`} onClick={() => { setRemoving(bot); busy.current = true; revision.current++ }}><Trash2 /></Button></div>
    </div>)}</div>}
    {dialogOpen && <BotDialog account={editing} accounts={accounts} onSaved={setAccounts} onClose={() => { setDialogOpen(false); busy.current = false }} />}
    <ConfirmDialog open={!!removing} onOpenChange={open => { if (!open && !saving) { setRemoving(undefined); busy.current = false } }} title="Remove WeCom bot?" message={`Remove ${removing?.name || 'this bot'} and its session bindings?`} confirmLabel="Remove" loading={saving} onConfirm={() => { void remove() }} />
  </section>
}
