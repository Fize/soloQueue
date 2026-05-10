import { useState } from 'react';
import type { AgentInfo, AgentState } from '@/types';
import { useAgentProfile } from '@/hooks/useAgentProfile';
import { useAgentConfig } from '@/hooks/useAgentConfig';
import { updateAgentConfig } from '@/lib/api';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ScrollArea } from '@/components/ui/scroll-area';
import { cn } from '@/lib/utils';
import { Settings, Save, Pencil, Eye, Loader2 } from 'lucide-react';

interface AgentDetailDialogProps {
  agent: AgentInfo | null;
  templateId?: string | null;
  templateName?: string | null;
  isL1?: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const stateVariant: Record<AgentState, 'default' | 'secondary' | 'outline' | 'destructive'> = {
  processing: 'default',
  idle: 'secondary',
  stopping: 'outline',
  stopped: 'outline',
};

const stateLabel: Record<AgentState, string> = {
  processing: '运行中',
  idle: '空闲',
  stopping: '停止中',
  stopped: '已停止',
};

// ─── Inline Editor ──────────────────────────────────────────────────────────

function InlineEditor({ content, onSave, saving, height = 'h-[400px]' }: {
  content: string;
  onSave: (draft: string) => Promise<void>;
  saving: boolean;
  height?: string;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(content);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    setError(null);
    try {
      await onSave(draft);
      setEditing(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed');
    }
  };

  const handleCancel = () => {
    setDraft(content);
    setEditing(false);
    setError(null);
  };

  // Sync draft when external content changes (e.g. after save or tab switch)
  if (!editing && draft !== content) {
    // Will be updated on next render
  }

  return (
    <div>
      <div className="flex items-center justify-end gap-1 mb-2">
        <Button
          size="sm"
          variant={editing ? 'outline' : 'default'}
          className="h-7 gap-1 text-xs"
          onClick={() => { setDraft(content); setEditing(false); }}
          disabled={!editing}
        >
          <Eye className="h-3 w-3" />
          Preview
        </Button>
        <Button
          size="sm"
          variant={editing ? 'default' : 'outline'}
          className="h-7 gap-1 text-xs"
          onClick={() => { setDraft(content); setEditing(true); }}
          disabled={editing}
        >
          <Pencil className="h-3 w-3" />
          Edit
        </Button>
      </div>

      {editing ? (
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          className={`w-full ${height} resize-y rounded-md border-2 border-border bg-card p-4 font-mono text-xs leading-relaxed focus:outline-none focus:ring-2 focus:ring-primary/50`}
          spellCheck={false}
        />
      ) : (
        <ScrollArea className={`${height} rounded-md border border-border bg-muted/30 p-4`}>
          {content ? (
            <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed">{content}</pre>
          ) : (
            <p className="text-sm text-muted-foreground">暂无内容</p>
          )}
        </ScrollArea>
      )}

      {editing && (
        <div className="mt-3 flex items-center gap-2">
          <Button size="sm" onClick={handleSave} disabled={saving || draft === content}>
            {saving ? (
              <><Loader2 className="mr-1 h-3 w-3 animate-spin" /> Saving...</>
            ) : (
              <><Save className="mr-1 h-3 w-3" /> Save</>
            )}
          </Button>
          <Button size="sm" variant="outline" onClick={handleCancel} disabled={saving}>
            Cancel
          </Button>
          {error && <span className="text-xs text-destructive">{error}</span>}
        </div>
      )}
    </div>
  );
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function AgentDetailDialog({ agent, templateId, templateName, isL1 = false, open, onOpenChange }: AgentDetailDialogProps) {
  const effectiveId = agent?.id ?? templateId ?? null;
  const effectiveName = agent?.name ?? templateName ?? '';
  const hasAgent = !!agent;

  const { profile, loading } = useAgentProfile(isL1 ? (agent?.id || 'main') : null);
  const { config, loading: configLoading, refetch } = useAgentConfig(!isL1 && effectiveId ? effectiveId : null);

  // Editing state — must be before any early return (Rules of Hooks).
  const [savingYaml, setSavingYaml] = useState(false);
  const [savingPrompt, setSavingPrompt] = useState(false);

  if (!agent && !templateId) return null;

  // L1 Agent 特殊展示：Soul 和 Rules
  if (isL1) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-[600px] max-h-[80vh]">
          <DialogHeader>
            <div className="flex items-center justify-between pr-8">
              <DialogTitle className="flex items-center gap-2">
                <span>{effectiveName}</span>
                <Badge variant="default" className="text-xs">主 Agent</Badge>
              </DialogTitle>
              <Button
                variant="outline"
                size="sm"
                onClick={() => { window.location.hash = 'settings/profile'; onOpenChange(false); }}
              >
                <Settings className="mr-1 h-3 w-3" />
                编辑
              </Button>
            </div>
            {hasAgent && (
              <p className="font-mono text-xs text-muted-foreground">{agent!.instance_id}</p>
            )}
          </DialogHeader>

          <Tabs defaultValue="soul" className="mt-2">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="soul">Soul</TabsTrigger>
              <TabsTrigger value="rules">Rules</TabsTrigger>
            </TabsList>

            <TabsContent value="soul" className="mt-3">
              <ScrollArea className="h-[400px] rounded-md border border-border p-4">
                {loading ? (
                  <p className="text-sm text-muted-foreground">加载中...</p>
                ) : profile?.soul ? (
                  <pre className="text-xs whitespace-pre-wrap font-mono leading-relaxed">{profile.soul}</pre>
                ) : (
                  <p className="text-sm text-muted-foreground">暂无 Soul 配置</p>
                )}
              </ScrollArea>
            </TabsContent>

            <TabsContent value="rules" className="mt-3">
              <ScrollArea className="h-[400px] rounded-md border border-border p-4">
                {loading ? (
                  <p className="text-sm text-muted-foreground">加载中...</p>
                ) : profile?.rules ? (
                  <pre className="text-xs whitespace-pre-wrap font-mono leading-relaxed">{profile.rules}</pre>
                ) : (
                  <p className="text-sm text-muted-foreground">暂无 Rules 配置</p>
                )}
              </ScrollArea>
            </TabsContent>
          </Tabs>
        </DialogContent>
      </Dialog>
    );
  }

  // 普通 Agent (L2/L3) — 可能无运行实例（仅模板）
  const handleSaveYaml = async (draft: string) => {
    setSavingYaml(true);
    try {
      await updateAgentConfig(effectiveId!, { raw_config: draft });
      refetch();
    } finally {
      setSavingYaml(false);
    }
  };

  const handleSavePrompt = async (draft: string) => {
    setSavingPrompt(true);
    try {
      await updateAgentConfig(effectiveId!, { system_prompt: draft });
      refetch();
    } finally {
      setSavingPrompt(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[600px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span>{effectiveName}</span>
            {hasAgent ? (
              <Badge variant={stateVariant[agent.state]} className="text-xs capitalize">
                {stateLabel[agent.state] || agent.state}
              </Badge>
            ) : (
              <Badge variant="outline" className="text-xs">未启动</Badge>
            )}
          </DialogTitle>
          {hasAgent && (
            <p className="font-mono text-xs text-muted-foreground">{agent.instance_id}</p>
          )}
        </DialogHeader>

        <Tabs defaultValue="status" className="mt-2">
          <TabsList className="grid w-full grid-cols-4">
            <TabsTrigger value="status" disabled={!hasAgent}>状态</TabsTrigger>
            <TabsTrigger value="details" disabled={!hasAgent}>详情</TabsTrigger>
            <TabsTrigger value="config">YAML</TabsTrigger>
            <TabsTrigger value="prompt">Prompt</TabsTrigger>
          </TabsList>

          {/* 状态 Tab */}
          <TabsContent value="status" className="space-y-3 mt-3">
            {hasAgent ? (
              <>
                <div className="space-y-1.5">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase">工作状态</h4>
                  <div className="rounded-md border border-border p-3 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">状态</span>
                      <Badge variant={stateVariant[agent.state]} className="text-xs">
                        {stateLabel[agent.state] || agent.state}
                      </Badge>
                    </div>
                    {agent.error_count > 0 && (
                      <div className="flex justify-between text-sm">
                        <span className="text-muted-foreground">错误次数</span>
                        <span className="text-destructive font-medium">{agent.error_count}</span>
                      </div>
                    )}
                    {agent.last_error && (
                      <div className="space-y-1">
                        <span className="text-xs text-muted-foreground">最后错误</span>
                        <p className="rounded-md bg-destructive/10 p-2 text-xs text-destructive break-all">
                          {agent.last_error}
                        </p>
                      </div>
                    )}
                  </div>
                </div>
                <div className="space-y-1.5">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase">工作负载</h4>
                  <div className="rounded-md border border-border p-3 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">待处理委托</span>
                      <span className="font-medium">{agent.pending_delegations}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">高优先级邮箱</span>
                      <span className="font-medium">{agent.mailbox_high}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">普通邮箱</span>
                      <span className="font-medium">{agent.mailbox_normal}</span>
                    </div>
                  </div>
                </div>
              </>
            ) : (
              <p className="text-sm text-muted-foreground py-8 text-center">Agent 未启动，暂无运行时状态</p>
            )}
          </TabsContent>

          {/* 详情 Tab */}
          <TabsContent value="details" className="space-y-3 mt-3">
            {hasAgent ? (
              <>
                <div className="space-y-1.5">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase">基本信息</h4>
                  <div className="rounded-md border border-border p-3 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">名称</span>
                      <span className="font-medium">{agent.name}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">模型</span>
                      <span className="font-mono text-xs">{agent.model_id}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">分组</span>
                      <span className="font-medium">{agent.group || 'Session'}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">任务级别</span>
                      <span className="font-medium">{agent.task_level || '-'}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">是否 Leader</span>
                      <span className={cn('font-medium', agent.is_leader ? 'text-primary' : '')}>
                        {agent.is_leader ? '是' : '否'}
                      </span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">错误次数</span>
                      <span className={cn('font-medium', agent.error_count > 0 ? 'text-destructive' : '')}>
                        {agent.error_count}
                      </span>
                    </div>
                  </div>
                </div>
                <div className="space-y-1.5">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase">标识</h4>
                  <div className="rounded-md border border-border p-3 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">ID</span>
                      <span className="font-mono text-xs text-muted-foreground">{agent.id}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">Instance ID</span>
                      <span className="font-mono text-xs text-muted-foreground">{agent.instance_id}</span>
                    </div>
                  </div>
                </div>
              </>
            ) : (
              <p className="text-sm text-muted-foreground py-8 text-center">Agent 未启动，暂无详情</p>
            )}
          </TabsContent>

          {/* YAML Tab — editable frontmatter config */}
          <TabsContent value="config" className="mt-3">
            {configLoading ? (
              <p className="text-sm text-muted-foreground">加载中...</p>
            ) : config ? (
              <InlineEditor
                content={config.raw_config || ''}
                onSave={handleSaveYaml}
                saving={savingYaml}
              />
            ) : (
              <p className="text-sm text-muted-foreground">暂无配置信息</p>
            )}
          </TabsContent>

          {/* Prompt Tab — editable markdown body / system prompt */}
          <TabsContent value="prompt" className="mt-3">
            {configLoading ? (
              <p className="text-sm text-muted-foreground">加载中...</p>
            ) : config ? (
              <InlineEditor
                content={config.system_prompt || ''}
                onSave={handleSavePrompt}
                saving={savingPrompt}
              />
            ) : (
              <p className="text-sm text-muted-foreground">暂无 Prompt 配置</p>
            )}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
