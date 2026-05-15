import { useState } from 'react';
import { useConfigStore } from '@/stores/configStore';
import { useToolsAndSkillsStore } from '@/stores/toolsAndSkillsStore';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Save, Plus, X, Wrench, FileText, Terminal, Globe, Search, ListChecks } from 'lucide-react';
import type { ToolsConfig, ToolInfo } from '@/types';

// ─── Tool icon mapping ────────────────────────────────────────────────────

function getToolIcon(name: string) {
  if (name.includes('file_read') || name.includes('read')) return FileText;
  if (name.includes('write') || name.includes('replace') || name.includes('multi')) return FileText;
  if (name.includes('glob')) return Search;
  if (name.includes('grep')) return Search;
  if (name.includes('shell') || name.includes('bash')) return Terminal;
  if (name.includes('http') || name.includes('fetch') || name.includes('web_search')) return Globe;
  if (name.includes('todo') || name.includes('plan')) return ListChecks;
  return Wrench;
}

function getParamCount(params: Record<string, unknown> | null): number {
  if (!params || !params.properties || typeof params.properties !== 'object') return 0;
  return Object.keys(params.properties as Record<string, unknown>).length;
}

function ToolCard({ tool }: { tool: ToolInfo }) {
  const Icon = getToolIcon(tool.name);
  const paramCount = getParamCount(tool.parameters);

  return (
    <div className="border rounded-lg bg-card p-3 shadow-sm hover:shadow-md hover:-translate-y-0.5">
      <div className="flex items-start gap-2.5">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-secondary">
          <Icon className="h-4 w-4 text-secondary-foreground" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <code className="text-xs font-bold text-foreground">{tool.name}</code>
            {paramCount > 0 && (
              <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                {paramCount} param{paramCount > 1 ? 's' : ''}
              </span>
            )}
          </div>
          <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground line-clamp-2">
            {tool.description || 'No description'}
          </p>
        </div>
      </div>
    </div>
  );
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function ToolsTab() {
  const config = useConfigStore((state) => state.config);
  const patch = useConfigStore((state) => state.patch);
  const saving = useConfigStore((state) => state.saving);
  const error = useConfigStore((state) => state.error);
  const toolList = useToolsAndSkillsStore((state) => state.tools);
  const toolsLoading = useToolsAndSkillsStore((state) => state.toolsLoading);

  const [toolsCfg, setToolsCfg] = useState<ToolsConfig | null>(null);
  const [newAllowedHost, setNewAllowedHost] = useState('');
  const [newBlockRegex, setNewBlockRegex] = useState('');
  const [newConfirmRegex, setNewConfirmRegex] = useState('');

  if (config && !toolsCfg) {
    setToolsCfg({
      ...config.tools,
      httpAllowedHosts: config.tools.httpAllowedHosts ? [...config.tools.httpAllowedHosts] : [],
      shellBlockRegexes: config.tools.shellBlockRegexes ? [...config.tools.shellBlockRegexes] : [],
      shellConfirmRegexes: config.tools.shellConfirmRegexes ? [...config.tools.shellConfirmRegexes] : [],
    });
  }

  if (!config || !toolsCfg) {
    return <div className="text-sm text-muted-foreground">Loading configuration...</div>;
  }

  const tools = toolsCfg;

  const handleSave = async () => {
    await patch({ tools });
  };

  const fmtBytes = (bytes: number) => {
    if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(0)} MiB`;
    if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(0)} KiB`;
    return `${bytes} B`;
  };

  const builtinTools = toolList?.tools ?? [];

  return (
    <div className="space-y-6">
      {/* Built-in Tools List */}
      <div className="border rounded-lg bg-card p-5 shadow-sm">
        <div className="mb-4 flex items-center gap-2">
          <Wrench className="h-4 w-4 text-foreground" />
          <h3 className="text-sm font-bold text-foreground">Built-in Tools</h3>
          <span className="rounded bg-secondary px-1.5 py-0.5 text-[10px] text-secondary-foreground">
            {builtinTools.length}
          </span>
        </div>
        {toolsLoading ? (
          <p className="text-xs text-muted-foreground">Loading tools...</p>
        ) : builtinTools.length === 0 ? (
          <p className="text-xs text-muted-foreground">No tools available</p>
        ) : (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {builtinTools.map((tool) => (
              <ToolCard key={tool.name} tool={tool} />
            ))}
          </div>
        )}
      </div>

      {/* File Read Limits */}
      <div className="border rounded-lg bg-card p-5 shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">File Read Limits</h3>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-end gap-2">
            <Input
              label="Max File Size"
              type="number"
              min={0}
              value={tools.maxFileSize}
              onChange={(e) => setToolsCfg({ ...tools, maxFileSize: Number(e.target.value) })}
              className="w-28 text-right"
            />
            <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.maxFileSize)})</span>
          </div>
          <Input
            label="Max Matches"
            type="number"
            min={0}
            value={tools.maxMatches}
            onChange={(e) => setToolsCfg({ ...tools, maxMatches: Number(e.target.value) })}
          />
          <Input
            label="Max Line Length"
            type="number"
            min={0}
            value={tools.maxLineLen}
            onChange={(e) => setToolsCfg({ ...tools, maxLineLen: Number(e.target.value) })}
          />
          <Input
            label="Max Glob Items"
            type="number"
            min={0}
            value={tools.maxGlobItems}
            onChange={(e) => setToolsCfg({ ...tools, maxGlobItems: Number(e.target.value) })}
          />
        </div>
        <div className="mt-4 flex items-center gap-3 border-t border-border pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Read Limits'}
          </Button>
        </div>
      </div>

      {/* File Write Limits */}
      <div className="border rounded-lg bg-card p-5 shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">File Write Limits</h3>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-end gap-2">
            <Input
              label="Max Write Size"
              type="number"
              min={0}
              value={tools.maxWriteSize}
              onChange={(e) => setToolsCfg({ ...tools, maxWriteSize: Number(e.target.value) })}
              className="w-28 text-right"
            />
            <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.maxWriteSize)})</span>
          </div>
          <div className="flex items-end gap-2">
            <Input
              label="Max Multi-Write Bytes"
              type="number"
              min={0}
              value={tools.maxMultiWriteBytes}
              onChange={(e) => setToolsCfg({ ...tools, maxMultiWriteBytes: Number(e.target.value) })}
              className="w-28 text-right"
            />
            <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.maxMultiWriteBytes)})</span>
          </div>
          <Input
            label="Max Multi-Write Files"
            type="number"
            min={0}
            value={tools.maxMultiWriteFiles}
            onChange={(e) => setToolsCfg({ ...tools, maxMultiWriteFiles: Number(e.target.value) })}
          />
          <Input
            label="Max Replace Edits"
            type="number"
            min={0}
            value={tools.maxReplaceEdits}
            onChange={(e) => setToolsCfg({ ...tools, maxReplaceEdits: Number(e.target.value) })}
          />
        </div>
        <div className="mt-4 flex items-center gap-3 border-t border-border pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Write Limits'}
          </Button>
        </div>
      </div>

      {/* HTTP Limits */}
      <div className="border rounded-lg bg-card p-5 shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">HTTP Limits</h3>
        <div className="grid gap-4">
          <div className="flex items-center justify-between">
            <div className="flex flex-col gap-1">
              <Label>Block Private IPs</Label>
              <span className="text-[10px] text-muted-foreground">Prevent requests to private/local network addresses</span>
            </div>
            <Switch checked={tools.httpBlockPrivate} onCheckedChange={(v) => setToolsCfg({ ...tools, httpBlockPrivate: v })} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="flex items-end gap-2">
              <Input
                label="Max Body Size"
                type="number"
                min={0}
                value={tools.httpMaxBody}
                onChange={(e) => setToolsCfg({ ...tools, httpMaxBody: Number(e.target.value) })}
                className="w-28 text-right"
              />
              <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.httpMaxBody)})</span>
            </div>
            <Input
              label="Timeout (ms)"
              type="number"
              min={0}
              value={tools.httpTimeoutMs}
              onChange={(e) => setToolsCfg({ ...tools, httpTimeoutMs: Number(e.target.value) })}
            />
          </div>
          <div>
            <Label className="mb-2 block">Allowed Hosts</Label>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {(tools.httpAllowedHosts || []).map((host, i) => (
                <span key={i} className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs">
                  {host}
                  <button onClick={() => setToolsCfg({ ...tools, httpAllowedHosts: tools.httpAllowedHosts?.filter((_, j) => j !== i) })}>
                    <X className="h-2.5 w-2.5 text-muted-foreground hover:text-foreground" />
                  </button>
                </span>
              ))}
            </div>
            <div className="flex gap-2">
              <Input placeholder="e.g. api.example.com" value={newAllowedHost} onChange={(e) => setNewAllowedHost(e.target.value)} className="flex-1" />
              <Button size="sm" variant="outline" onClick={() => {
                if (newAllowedHost.trim()) {
                  setToolsCfg({ ...tools, httpAllowedHosts: [...(tools.httpAllowedHosts || []), newAllowedHost.trim()] });
                  setNewAllowedHost('');
                }
              }}>
                <Plus className="h-3 w-3" />
              </Button>
            </div>
          </div>
        </div>
        <div className="mt-4 flex items-center gap-3 border-t border-border pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save HTTP Limits'}
          </Button>
        </div>
      </div>

      {/* Shell Limits */}
      <div className="border rounded-lg bg-card p-5 shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Shell Limits</h3>
        <div className="grid gap-4">
          <div className="flex items-end gap-2">
            <Input
              label="Max Output"
              type="number"
              min={0}
              value={tools.shellMaxOutput}
              onChange={(e) => setToolsCfg({ ...tools, shellMaxOutput: Number(e.target.value) })}
              className="w-28 text-right"
            />
            <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.shellMaxOutput)})</span>
          </div>

          <div>
            <Label className="mb-2 block">Block Regexes</Label>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {(tools.shellBlockRegexes || []).map((rx, i) => (
                <span key={i} className="inline-flex items-center gap-1 rounded-md bg-destructive/10 px-2 py-0.5 font-mono text-xs text-destructive">
                  {rx}
                  <button onClick={() => setToolsCfg({ ...tools, shellBlockRegexes: tools.shellBlockRegexes?.filter((_, j) => j !== i) })}>
                    <X className="h-2.5 w-2.5 hover:text-foreground" />
                  </button>
                </span>
              ))}
            </div>
            <div className="flex gap-2">
              <Input placeholder="e.g. ^\\s*rm\\b" value={newBlockRegex} onChange={(e) => setNewBlockRegex(e.target.value)} className="flex-1 font-mono" />
              <Button size="sm" variant="outline" onClick={() => {
                if (newBlockRegex.trim()) {
                  setToolsCfg({ ...tools, shellBlockRegexes: [...(tools.shellBlockRegexes || []), newBlockRegex.trim()] });
                  setNewBlockRegex('');
                }
              }}>
                <Plus className="h-3 w-3" />
              </Button>
            </div>
          </div>

          <div>
            <Label className="mb-2 block">Confirm Regexes</Label>
            <span className="mb-1.5 block text-[10px] text-muted-foreground">Commands matching these patterns require user confirmation</span>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {(tools.shellConfirmRegexes || []).map((rx, i) => (
                <span key={i} className="inline-flex items-center gap-1 rounded-md bg-warning/10 px-2 py-0.5 font-mono text-xs text-warning">
                  {rx}
                  <button onClick={() => setToolsCfg({ ...tools, shellConfirmRegexes: tools.shellConfirmRegexes?.filter((_, j) => j !== i) })}>
                    <X className="h-2.5 w-2.5 hover:text-foreground" />
                  </button>
                </span>
              ))}
            </div>
            <div className="flex gap-2">
              <Input placeholder="e.g. ^\\s*rm\\b" value={newConfirmRegex} onChange={(e) => setNewConfirmRegex(e.target.value)} className="flex-1 font-mono" />
              <Button size="sm" variant="outline" onClick={() => {
                if (newConfirmRegex.trim()) {
                  setToolsCfg({ ...tools, shellConfirmRegexes: [...(tools.shellConfirmRegexes || []), newConfirmRegex.trim()] });
                  setNewConfirmRegex('');
                }
              }}>
                <Plus className="h-3 w-3" />
              </Button>
            </div>
          </div>
        </div>
        <div className="mt-4 flex items-center gap-3 border-t border-border pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Shell Limits'}
          </Button>
        </div>
      </div>

      {/* WebSearch */}
      <div className="border rounded-lg bg-card p-5 shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Web Search</h3>
        <Input
          label="Timeout (ms)"
          type="number"
          min={0}
          value={tools.webSearchTimeoutMs}
          onChange={(e) => setToolsCfg({ ...tools, webSearchTimeoutMs: Number(e.target.value) })}
          className="w-40"
        />
        <div className="mt-4 flex items-center gap-3 border-t border-border pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save WebSearch'}
          </Button>
          {error && <span className="text-[10px] text-destructive">{error}</span>}
        </div>
      </div>
    </div>
  );
}
