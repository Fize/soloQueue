import { useState, useEffect, useCallback } from 'react'
import { getAgentProfile, updateAgentProfile, getQQBotsConfig, getWeChatBotsConfig } from '@/lib/api'
import type { AgentProfile } from '@/types'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { Save, Heart, Scale, Eye, Pencil, Loader2 } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'

// ─── Editor Section ────────────────────────────────────────────────────────

interface EditorSectionProps {
  title: string
  icon: typeof Heart
  content: string
  onSave: (content: string) => Promise<void>
  saving: boolean
}

function EditorSection({ title, icon: Icon, content, onSave, saving }: EditorSectionProps) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(content)
  const [saveError, setSaveError] = useState<string | null>(null)
  const { t } = useTranslation()

  // Sync draft when content changes externally (e.g. after save)
  useEffect(() => {
    setDraft(content)
  }, [content])

  const lineCount = draft.split('\n').length
  const charCount = draft.length

  const handleSave = async () => {
    setSaveError(null)
    try {
      await onSave(draft)
      setEditing(false)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Save failed')
    }
  }

  const handleCancel = () => {
    setDraft(content)
    setEditing(false)
    setSaveError(null)
  }

  return (
    <div className="border rounded-lg bg-card p-5 shadow-sm">
      {/* Header */}
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <Icon className="h-4 w-4 shrink-0 text-foreground" />
          <h3 className="text-sm font-bold text-foreground">{title}</h3>
          <Badge variant="secondary" className="text-[10px] shrink-0">
            {t('common.lineCount', { count: lineCount })} · {t('common.charCount', { count: charCount })}
          </Badge>
        </div>

        {/* Edit / Preview toggle */}
        <div className="flex items-center gap-1 shrink-0">
          <Button
            size="sm"
            variant={editing ? 'outline' : 'default'}
            className="h-7 gap-1 text-xs"
            onClick={() => setEditing(false)}
            disabled={!editing}
          >
            <Eye className="h-3 w-3" />
            {t('common.preview')}
          </Button>
          <Button
            size="sm"
            variant={editing ? 'default' : 'outline'}
            className="h-7 gap-1 text-xs"
            onClick={() => setEditing(true)}
            disabled={editing}
          >
            <Pencil className="h-3 w-3" />
            {t('common.edit')}
          </Button>
        </div>
      </div>

      {/* Content area */}
      {editing ? (
        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          className="min-h-[400px] font-mono text-xs leading-relaxed"
          rows={16}
          spellCheck={false}
        />
      ) : (
        <ScrollArea className="h-[400px] rounded-md border border-border bg-card p-4">
          {content ? (
            <MarkdownPreview content={content} />
          ) : (
            <p className="text-sm text-muted-foreground">{t('common.none')}</p>
          )}
        </ScrollArea>
      )}

      {/* Footer */}
      {editing && (
        <div className="mt-4 flex items-center gap-3 border-t border-border pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving || draft === content}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" /> {t('common.saving')}
              </>
            ) : (
              <>
                <Save className="mr-1 h-3 w-3" /> {t('common.save')} {title}
              </>
            )}
          </Button>
          <Button size="sm" variant="outline" onClick={handleCancel} disabled={saving}>
            {t('common.cancel')}
          </Button>
          {saveError && <span className="text-[10px] text-destructive">{saveError}</span>}
        </div>
      )}
    </div>
  )
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function ProfileTab() {
  const [profile, setProfile] = useState<AgentProfile | null>(null)
  const [loading, setLoading] = useState(true)
  const [savingSoul, setSavingSoul] = useState(false)
  const [savingRules, setSavingRules] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [qqChannel, setQqChannel] = useState('')
  const [wechatChannel, setWechatChannel] = useState('')
  const [notifyChannel, setNotifyChannel] = useState('')
  const [savingChannels, setSavingChannels] = useState(false)
  const [qqBotOptions, setQqBotOptions] = useState<{ id: string; name: string }[]>([])
  const [wechatBotOptions, setWechatBotOptions] = useState<{ id: string; name: string }[]>([])
  const { t } = useTranslation()

  const fetchProfile = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const data = await getAgentProfile('main')
      setProfile(data)
      const ch = data.channels || {}
      setQqChannel(ch.qq || '')
      setWechatChannel(ch.wechat || '')
      setNotifyChannel(data.notify_channel || '')
    } catch {
      setError('Failed to load profile')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchProfile()
    getQQBotsConfig()
      .then((bots) => setQqBotOptions(bots.map(b => ({ id: b.id || '', name: b.name || '' }))))
      .catch(() => {})
    getWeChatBotsConfig()
      .then((bots) => setWechatBotOptions(bots.map(b => ({ id: b.id, name: b.name }))))
      .catch(() => {})
  }, [fetchProfile])

  const handleSaveSoul = async (soul: string) => {
    setSavingSoul(true)
    try {
      const updated = await updateAgentProfile('main', { soul })
      setProfile(updated)
    } finally {
      setSavingSoul(false)
    }
  }

  const handleSaveRules = async (rules: string) => {
    setSavingRules(true)
    try {
      const updated = await updateAgentProfile('main', { rules })
      setProfile(updated)
    } finally {
      setSavingRules(false)
    }
  }

  const handleSaveChannels = async () => {
    setSavingChannels(true)
    try {
      const channels: Record<string, string> = {}
      if (qqChannel) channels.qq = qqChannel
      if (wechatChannel) channels.wechat = wechatChannel
      await updateAgentProfile('main', {
        channels: Object.keys(channels).length > 0 ? channels : null,
        notify_channel: notifyChannel || null,
      })
      await fetchProfile()
    } finally {
      setSavingChannels(false)
    }
  }

  if (loading) {
    return <div className="text-sm text-muted-foreground">{t('common.loading')}</div>
  }

  if (error) {
    return <div className="text-sm text-destructive">{t('common.error')}</div>
  }

  if (!profile) {
    return <div className="text-sm text-muted-foreground">{t('common.none')}</div>
  }

  return (
    <div className="space-y-6">
      {/* L1 Notification Channel Config */}
      {(qqBotOptions.length > 0 || wechatBotOptions.length > 0) && (
        <div className="border rounded-lg bg-card p-5 shadow-sm">
          <h3 className="text-sm font-bold mb-3">通知渠道 (Notification Channels)</h3>
          <p className="text-xs text-muted-foreground mb-4">
            配置 L1 主助手的消息通知渠道。定时任务完成后将通过绑定渠道通知用户。
          </p>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-3">
            {qqBotOptions.length > 0 && (
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">QQ Bot</label>
                <select
                  value={qqChannel}
                  onChange={(e) => setQqChannel(e.target.value)}
                  className="w-full px-2.5 py-1.5 rounded-md border border-border bg-background text-xs"
                >
                  <option value="">无</option>
                  {qqBotOptions.map(bot => (
                    <option key={bot.id} value={bot.id}>{bot.name} ({bot.id})</option>
                  ))}
                </select>
              </div>
            )}
            {wechatBotOptions.length > 0 && (
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground">WeChat</label>
                <select
                  value={wechatChannel}
                  onChange={(e) => setWechatChannel(e.target.value)}
                  className="w-full px-2.5 py-1.5 rounded-md border border-border bg-background text-xs"
                >
                  <option value="">无</option>
                  {wechatBotOptions.map(bot => (
                    <option key={bot.id} value={bot.id}>{bot.name} ({bot.id})</option>
                  ))}
                </select>
              </div>
            )}
          </div>

          {(qqChannel || wechatChannel) && (
            <div className="flex flex-col gap-1.5 mb-3 pt-2 border-t border-border/50">
              <label className="text-xs text-muted-foreground">通知通道 (用于定时任务结果通知)</label>
              <div className="flex gap-4">
                {qqChannel && (
                  <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                    <input type="radio" name="l1NotifyChannel" value="qq"
                      checked={notifyChannel === 'qq'} onChange={() => setNotifyChannel('qq')} className="h-3 w-3" />
                    QQ Bot
                  </label>
                )}
                {wechatChannel && (
                  <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                    <input type="radio" name="l1NotifyChannel" value="wechat"
                      checked={notifyChannel === 'wechat'} onChange={() => setNotifyChannel('wechat')} className="h-3 w-3" />
                    WeChat
                  </label>
                )}
                <label className="flex items-center gap-1.5 text-xs cursor-pointer text-muted-foreground">
                  <input type="radio" name="l1NotifyChannel" value=""
                    checked={notifyChannel === ''} onChange={() => setNotifyChannel('')} className="h-3 w-3" />
                  无 (使用第一个绑定渠道)
                </label>
              </div>
            </div>
          )}

          <div className="flex items-center gap-3 border-t border-border pt-3">
            <Button size="sm" onClick={handleSaveChannels} disabled={savingChannels}>
              {savingChannels ? (
                <><Loader2 className="mr-1 h-3 w-3 animate-spin" /> 保存中...</>
              ) : (
                <><Save className="mr-1 h-3 w-3" /> 保存渠道配置</>
              )}
            </Button>
          </div>
        </div>
      )}

      <EditorSection
        title={t('profile.soul')}
        icon={Heart}
        content={profile.soul}
        onSave={handleSaveSoul}
        saving={savingSoul}
      />
      <EditorSection
        title={t('profile.rules')}
        icon={Scale}
        content={profile.rules}
        onSave={handleSaveRules}
        saving={savingRules}
      />
    </div>
  )
}
