import { useState } from 'react';
import { useConfigStore } from '@/stores/configStore';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Save, Eye, EyeOff } from 'lucide-react';
import type { QQBotConfig } from '@/types';

export function IntegrationsTab() {
  const config = useConfigStore((state) => state.config);
  const patch = useConfigStore((state) => state.patch);
  const saving = useConfigStore((state) => state.saving);
  const error = useConfigStore((state) => state.error);

  const [qqbot, setQQBot] = useState<QQBotConfig | null>(null);
  const [showSecret, setShowSecret] = useState(false);

  if (config && !qqbot) {
    setQQBot({ ...config.qqbot });
  }

  if (!config || !qqbot) {
    return <div className="text-sm text-muted-foreground">Loading configuration...</div>;
  }

  const handleSave = async () => {
    await patch({ qqbot });
  };

  return (
    <div className="space-y-6">
      {/* QQ Bot */}
      <div className="border rounded-lg bg-card p-5 shadow-sm">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h3 className="text-sm font-bold text-foreground">QQ Bot</h3>
            <p className="text-[10px] text-muted-foreground">WebSocket Gateway integration for QQ Bot</p>
          </div>
          <Switch checked={qqbot.enabled} onCheckedChange={(v) => setQQBot({ ...qqbot, enabled: v })} />
        </div>

        {qqbot.enabled && (
          <div className="grid gap-4">
            <Input
              label="App ID"
              value={qqbot.appId}
              onChange={(e) => setQQBot({ ...qqbot, appId: e.target.value })}
              placeholder="Your QQ Bot App ID"
            />
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="appSecret">App Secret</Label>
              <div className="flex gap-2">
                <Input
                  id="appSecret"
                  type={showSecret ? 'text' : 'password'}
                  value={qqbot.appSecret}
                  onChange={(e) => setQQBot({ ...qqbot, appSecret: e.target.value })}
                  placeholder="Your QQ Bot App Secret"
                  className="flex-1"
                />
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setShowSecret(!showSecret)}
                  className="shrink-0"
                >
                  {showSecret ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                </Button>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Input
                label="Intents"
                type="number"
                min={0}
                value={qqbot.intents}
                onChange={(e) => setQQBot({ ...qqbot, intents: Number(e.target.value) })}
              />
              <div className="flex items-center justify-between pt-5">
                <div className="flex flex-col gap-1">
                  <Label>Sandbox Mode</Label>
                  <span className="text-[10px] text-muted-foreground">Use sandbox API endpoint</span>
                </div>
                <Switch checked={qqbot.sandbox} onCheckedChange={(v) => setQQBot({ ...qqbot, sandbox: v })} />
              </div>
            </div>
          </div>
        )}

        <div className="mt-4 flex items-center gap-3 border-t border-border pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save QQ Bot'}
          </Button>
          {error && <span className="text-[10px] text-destructive">{error}</span>}
        </div>
      </div>
    </div>
  );
}
