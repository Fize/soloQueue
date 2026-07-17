import { useCallback, useEffect, useRef, useState } from 'react'
import { CircleCheck, Clock3, Loader2, MessageCircle, Plus, RefreshCw, ScanLine, Trash2 } from 'lucide-react'
import QRCode from 'qrcode'
import { toast } from 'sonner'

import {
  cancelWeChatLogin,
  deleteWeChatBot,
  getQQBotsConfig,
  getWeChatBotsConfig,
  getWeChatLogin,
  startWeChatLogin,
  submitWeChatVerification,
  updateQQBotsConfig,
  updateWeChatBotsConfig,
} from '@/lib/api'
import type { QQBotConfig, WeChatAccountView, WeChatLoginSnapshot } from '@/types'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Select } from '@/components/ui/select'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { QQBotSection } from './ConfigTab/QQBotSection'

const terminalLoginStatuses = new Set(['connected', 'already_connected', 'expired', 'cancelled', 'failed'])

function QRCodeCanvas({ value, label, errorText }: { value: string; label: string; errorText: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const [renderError, setRenderError] = useState(false)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    void QRCode.toCanvas(canvas, value, {
      width: 232,
      margin: 2,
      errorCorrectionLevel: 'M',
      // These are QR matrix colors, not UI theme colors. Fixed contrast is
      // required so the code remains scannable in both app themes.
      color: {
        dark: '#111827',
        light: '#FFFFFF',
      },
    }).then(() => setRenderError(false)).catch(() => setRenderError(true))
  }, [value])

  return (
    <div className="mx-auto flex w-fit max-w-full items-center justify-center rounded-md border border-border bg-primary-foreground p-2">
      <canvas ref={canvasRef} role="img" aria-label={label} className={renderError ? 'hidden' : 'block size-[232px] max-w-full'} />
      {renderError && <p className="text-sm text-destructive">{errorText}</p>}
    </div>
  )
}

interface WeChatLoginDialogProps {
  open: boolean
  account?: WeChatAccountView
  onOpenChange: (open: boolean) => void
  onConnected: () => void
}

function WeChatLoginDialog({ open, account, onOpenChange, onConnected }: WeChatLoginDialogProps) {
  const { t } = useTranslation()
  const [accountId, setAccountId] = useState(account?.id || 'personal')
  const [name, setName] = useState(account?.name || 'WeChat')
  const [bindType, setBindType] = useState<'l1' | 'l2'>(account?.bind_type || 'l1')
  const [bindAgent, setBindAgent] = useState(account?.bind_agent || '')
  const [snapshot, setSnapshot] = useState<WeChatLoginSnapshot | null>(null)
  const [verificationCode, setVerificationCode] = useState('')
  const [working, setWorking] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!snapshot || terminalLoginStatuses.has(snapshot.status)) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [snapshot])

  useEffect(() => {
    if (!open || !snapshot || terminalLoginStatuses.has(snapshot.status)) return
    const timer = window.setTimeout(async () => {
      try {
        const next = await getWeChatLogin(snapshot.sessionId)
        setSnapshot(next)
        if (next.status === 'connected') {
          toast.success(t('channels.connected'))
          onConnected()
        }
      } catch (error) {
        toast.error((error as Error).message)
      }
    }, 1000)
    return () => window.clearTimeout(timer)
  }, [open, snapshot, onConnected, t])

  const statusText = snapshot ? (() => {
    switch (snapshot.status) {
      case 'awaiting_scan': return t('channels.awaitingScan')
      case 'scanned':
      case 'awaiting_confirmation': return t('channels.awaitingConfirmation')
      case 'awaiting_verification': return t('channels.awaitingVerification')
      case 'connected': return t('channels.connected')
      case 'already_connected': return t('channels.alreadyConnected')
      case 'expired': return t('channels.expired')
      case 'cancelled': return t('channels.cancelled')
      case 'failed': return t('channels.failed')
      default: return snapshot.message || snapshot.status
    }
  })() : ''

  const secondsRemaining = snapshot
    ? Math.max(0, Math.ceil((new Date(snapshot.expiresAt).getTime() - now) / 1000))
    : 0

  const beginLogin = async () => {
    if (!accountId.trim() || (bindType === 'l2' && !bindAgent.trim())) return
    setWorking(true)
    try {
      const next = await startWeChatLogin({
        accountId: accountId.trim(),
        name: name.trim() || 'WeChat',
        bindType,
        bindAgent: bindType === 'l2' ? bindAgent.trim() : undefined,
      })
      sessionStorage.setItem('soloqueue_wechat_login', next.sessionId)
      setSnapshot(next)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setWorking(false)
    }
  }

  const verify = async () => {
    if (!snapshot || !verificationCode.trim()) return
    setWorking(true)
    try {
      await submitWeChatVerification(snapshot.sessionId, verificationCode.trim())
      setSnapshot(await getWeChatLogin(snapshot.sessionId))
      setVerificationCode('')
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setWorking(false)
    }
  }

  const cancel = async () => {
    if (snapshot && !terminalLoginStatuses.has(snapshot.status)) {
      await cancelWeChatLogin(snapshot.sessionId).catch(() => undefined)
    }
    sessionStorage.removeItem('soloqueue_wechat_login')
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) void cancel() }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{account ? t('channels.reconnectTitle') : t('channels.connect')}</DialogTitle>
          <DialogDescription>{t('channels.credentialNote')}</DialogDescription>
        </DialogHeader>

        {!snapshot ? (
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="wechat-account-id">{t('channels.accountId')}</Label>
              <Input id="wechat-account-id" value={accountId} disabled={!!account} onChange={(event) => setAccountId(event.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="wechat-name">{t('channels.displayName')}</Label>
              <Input id="wechat-name" value={name} onChange={(event) => setName(event.target.value)} />
            </div>
            <div className="space-y-2">
              <Select
                id="wechat-binding"
                label={t('channels.sessionBinding')}
                value={bindType}
                onChange={(value) => setBindType(value as 'l1' | 'l2')}
                options={[
                  { value: 'l1', label: t('channels.l1Orchestrator') },
                  { value: 'l2', label: t('channels.l2Agent') },
                ]}
              />
            </div>
            {bindType === 'l2' && (
              <div className="space-y-2">
                <Label htmlFor="wechat-agent">{t('channels.agentId')}</Label>
                <Input id="wechat-agent" value={bindAgent} onChange={(event) => setBindAgent(event.target.value)} />
              </div>
            )}
          </div>
        ) : (
          <div className="space-y-4 rounded-md border border-border bg-muted/20 p-4 text-center" aria-live="polite">
            {snapshot.qrPayload && (
              <QRCodeCanvas value={snapshot.qrPayload} label={t('channels.scanQr')} errorText={t('channels.renderFailed')} />
            )}
            <div className="space-y-1.5">
              <div className="flex items-center justify-center gap-2 font-medium text-foreground">
                {snapshot.status === 'connected' ? <CircleCheck className="size-4 text-success" /> : <ScanLine className="size-4 text-primary" />}
                {statusText}
              </div>
              {snapshot.status === 'awaiting_scan' && <p className="text-xs text-muted-foreground">{t('channels.scanHint')}</p>}
              {!terminalLoginStatuses.has(snapshot.status) && (
                <p className="flex items-center justify-center gap-1 text-xs text-muted-foreground">
                  <Clock3 className="size-3.5" />{t('channels.remaining', { seconds: secondsRemaining })}
                </p>
              )}
            </div>
            {snapshot.status === 'awaiting_verification' && (
              <div className="flex gap-2">
                <Input aria-label={t('channels.verificationCode')} inputMode="numeric" value={verificationCode} onChange={(event) => setVerificationCode(event.target.value)} />
                <Button onClick={verify} disabled={working || !verificationCode.trim()}>{working ? t('channels.verifying') : t('channels.verify')}</Button>
              </div>
            )}
            {(snapshot.status === 'expired' || snapshot.status === 'failed') && (
              <Button variant="outline" onClick={() => setSnapshot(null)}><RefreshCw />{t('channels.generateNewQr')}</Button>
            )}
          </div>
        )}

        <DialogFooter>
          {!snapshot && <Button onClick={beginLogin} disabled={working || !accountId.trim() || (bindType === 'l2' && !bindAgent.trim())}>{working && <Loader2 className="animate-spin" />}{t('channels.generateQr')}</Button>}
          <Button variant="outline" onClick={cancel}>{t('common.cancel')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ChannelsTab() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [qqbots, setQQBots] = useState<QQBotConfig[]>([])
  const [wechatAccounts, setWechatAccounts] = useState<WeChatAccountView[]>([])
  const [loginOpen, setLoginOpen] = useState(false)
  const [loginAccount, setLoginAccount] = useState<WeChatAccountView | undefined>()
  const [removeAccount, setRemoveAccount] = useState<WeChatAccountView | null>(null)
  const [removing, setRemoving] = useState(false)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [qq, wechat] = await Promise.all([getQQBotsConfig(), getWeChatBotsConfig()])
      setQQBots(qq || [])
      setWechatAccounts(wechat || [])
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    const timer = window.setTimeout(() => { void loadData() }, 0)
    return () => window.clearTimeout(timer)
  }, [loadData])

  const saveQQ = async () => {
    try {
      await updateQQBotsConfig(qqbots)
      toast.success(t('config.toastQQBotsUpdated'))
      await loadData()
    } catch (error) {
      toast.error((error as Error).message)
    }
  }

  const saveWeChat = async () => {
    try {
      setWechatAccounts(await updateWeChatBotsConfig(wechatAccounts))
      toast.success(t('channels.settingsUpdated'))
    } catch (error) {
      toast.error((error as Error).message)
    }
  }

  const removeWeChat = async () => {
    if (!removeAccount) return
    setRemoving(true)
    try {
      await deleteWeChatBot(removeAccount.id)
      setRemoveAccount(null)
      await loadData()
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setRemoving(false)
    }
  }

  if (loading) return <div className="p-6 font-mono text-sm text-muted-foreground">{t('common.loading')}</div>

  return (
    <div className="space-y-8 pb-10">
      <QQBotSection config={qqbots} onChange={setQQBots} onSave={saveQQ} />

      <section className="space-y-6 rounded-xl border border-border bg-card p-6 shadow-sm">
        <div className="flex items-center justify-between gap-4 border-b border-border pb-3">
          <div className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <MessageCircle className="size-4 text-primary" />
              <h2 className="font-semibold text-foreground">{t('channels.wechat')}</h2>
            </div>
            <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{t('channels.authorizationDesc')}</p>
          </div>
          <Button size="sm" onClick={() => { setLoginAccount(undefined); setLoginOpen(true) }}><Plus />{t('channels.connect')}</Button>
        </div>

        {wechatAccounts.length === 0 ? (
          <div className="rounded-md border border-dashed border-border bg-muted/20 py-6 text-center text-sm text-muted-foreground">{t('channels.noAccounts')}</div>
        ) : (
          <div className="space-y-3">
            {wechatAccounts.map((account, index) => (
              <div key={account.id} className="rounded-md border border-border bg-muted/20 p-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <div className="font-medium">{account.name || account.id}</div>
                    <div className="mt-1 text-xs text-muted-foreground">{account.botIdMasked || t('channels.notConnected')} · {account.bind_type?.toUpperCase() || 'L1'}</div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch checked={account.enabled} onCheckedChange={(checked) => setWechatAccounts((items) => items.map((item, itemIndex) => itemIndex === index ? { ...item, enabled: checked } : item))} />
                    <Button size="xs" variant="outline" onClick={() => { setLoginAccount(account); setLoginOpen(true) }}><RefreshCw />{t('channels.reconnect')}</Button>
                    <Button size="icon-xs" variant="ghost" aria-label={`${t('channels.remove')} ${account.name}`} onClick={() => setRemoveAccount(account)}><Trash2 /></Button>
                  </div>
                </div>
              </div>
            ))}
            <div className="flex justify-end"><Button size="sm" onClick={saveWeChat}>{t('channels.saveSettings')}</Button></div>
          </div>
        )}
      </section>

      {loginOpen && <WeChatLoginDialog open account={loginAccount} onOpenChange={setLoginOpen} onConnected={loadData} />}
      <ConfirmDialog
        open={!!removeAccount}
        onOpenChange={(open) => { if (!open) setRemoveAccount(null) }}
        title={t('channels.removeTitle')}
        message={t('channels.removeMessage', { name: removeAccount?.name || removeAccount?.id || '' })}
        confirmLabel={t('channels.remove')}
        loading={removing}
        onConfirm={() => { void removeWeChat() }}
      />
    </div>
  )
}

export default ChannelsTab
