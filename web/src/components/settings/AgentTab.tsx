import { useState, useEffect } from 'react';
import { useConfig } from '@/hooks/useConfig';
import { useMCPConfig } from '@/hooks/useMCPConfig';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Save } from 'lucide-react';

export function AgentTab() {
  const { config, patch, saving, error } = useConfig();
  const { config: mcpConfig } = useMCPConfig();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [initialized, setInitialized] = useState(false);

  useEffect(() => {
    if (config && mcpConfig && !initialized) {
      const allowed = config.agent?.mcpServers ?? [];
      const allNames = Object.keys(mcpConfig.mcpServers ?? {});
      if (allowed.length === 0) {
        setSelected(new Set(allNames));
      } else {
        setSelected(new Set(allowed));
      }
      setInitialized(true);
    }
  }, [config, mcpConfig, initialized]);

  if (!config || !mcpConfig || !initialized) {
    return <div className="text-sm text-muted-foreground">Loading agent configuration...</div>;
  }

  const serverNames = Object.keys(mcpConfig.mcpServers ?? {}).sort();

  const allEnabled = serverNames.length > 0 && serverNames.every((name) => selected.has(name));

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const toggleAll = () => {
    if (allEnabled) {
      setSelected(new Set());
    } else {
      setSelected(new Set(serverNames));
    }
  };

  const handleSave = async () => {
    const mcpServers = allEnabled ? [] : [...selected];
    await patch({ agent: { mcpServers } });
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-sm font-bold text-foreground">L1 Orchestrator</h2>
        <p className="text-[10px] text-muted-foreground">
          Choose which MCP servers the L1 orchestrator can access. All toggled = access to every
          enabled server in the MCP configuration.
        </p>
      </div>

      {serverNames.length === 0 && (
        <div className="text-sm text-muted-foreground py-8 text-center nb-border rounded-lg">
          No MCP servers configured. Add servers in the MCP tab first.
        </div>
      )}

      {serverNames.length > 0 && (
        <>
          <div className="space-y-2">
            <div className="flex items-center justify-between nb-border rounded-lg bg-card p-3">
              <span className="text-sm font-medium">All Servers</span>
              <Switch checked={allEnabled} onCheckedChange={toggleAll} />
            </div>
            {serverNames.map((name) => {
              const serverConfig = mcpConfig.mcpServers[name];
              const globallyEnabled = serverConfig?.enabled ?? true;
              return (
                <div
                  key={name}
                  className="flex items-center justify-between nb-border rounded-lg bg-card p-3"
                >
                  <div className="flex flex-col">
                    <span className="text-sm font-medium">{name}</span>
                    {!globallyEnabled && (
                      <span className="text-[10px] text-muted-foreground">
                        Disabled in MCP configuration
                      </span>
                    )}
                  </div>
                  <Switch
                    checked={selected.has(name)}
                    disabled={!globallyEnabled}
                    onCheckedChange={() => toggle(name)}
                  />
                </div>
              );
            })}
          </div>

          <div className="flex items-center gap-3">
            <Button size="sm" onClick={handleSave} disabled={saving}>
              <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save'}
            </Button>
            {error && <span className="text-[10px] text-destructive">{error}</span>}
          </div>
        </>
      )}
    </div>
  );
}
