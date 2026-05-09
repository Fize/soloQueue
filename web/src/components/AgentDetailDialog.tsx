import type { AgentInfo, AgentState } from '@/types';
import { useAgentProfile } from '@/hooks/useAgentProfile';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Badge } from '@/components/ui/badge';
import { ScrollArea } from '@/components/ui/scroll-area';
import { cn } from '@/lib/utils';

interface AgentDetailDialogProps {
  agent: AgentInfo | null;
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

export function AgentDetailDialog({ agent, isL1 = false, open, onOpenChange }: AgentDetailDialogProps) {
  const { profile, loading } = useAgentProfile(isL1 && agent ? agent.id : null);

  if (!agent) return null;

  // L1 Agent 特殊展示：Soul 和 Rules
  if (isL1) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-[600px] max-h-[80vh]">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <span>{agent.name}</span>
              <Badge variant="default" className="text-xs">主 Agent</Badge>
            </DialogTitle>
            <p className="font-mono text-xs text-muted-foreground">{agent.instance_id}</p>
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

  // 普通 Agent 展示：状态和详情
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span>{agent.name}</span>
            <Badge variant={stateVariant[agent.state]} className="text-xs capitalize">
              {stateLabel[agent.state] || agent.state}
            </Badge>
          </DialogTitle>
          <p className="font-mono text-xs text-muted-foreground">{agent.instance_id}</p>
        </DialogHeader>

        <Tabs defaultValue="status" className="mt-2">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="status">状态</TabsTrigger>
            <TabsTrigger value="details">详情</TabsTrigger>
          </TabsList>

          {/* 状态 Tab */}
          <TabsContent value="status" className="space-y-3 mt-3">
            {/* 工作状态 */}
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

            {/* 工作负载 */}
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
          </TabsContent>

          {/* 详情 Tab */}
          <TabsContent value="details" className="space-y-3 mt-3">
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
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
