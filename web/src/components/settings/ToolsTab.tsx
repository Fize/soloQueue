import { useState } from 'react';
import { useConfig } from '@/hooks/useConfig';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Save, Plus, X } from 'lucide-react';
import type { ToolsConfig } from '@/types';

export function ToolsTab() {
  const { config, patch, saving, error } = useConfig();

  const [tools, setTools] = useState<ToolsConfig | null>(null);
  const [newAllowedHost, setNewAllowedHost] = useState('');
  const [newBlockRegex, setNewBlockRegex] = useState('');
  const [newConfirmRegex, setNewConfirmRegex] = useState('');

  if (config && !tools) {
    setTools({
      ...config.tools,
      httpAllowedHosts: config.tools.httpAllowedHosts ? [...config.tools.httpAllowedHosts] : [],
      shellBlockRegexes: config.tools.shellBlockRegexes ? [...config.tools.shellBlockRegexes] : [],
      shellConfirmRegexes: config.tools.shellConfirmRegexes ? [...config.tools.shellConfirmRegexes] : [],
    });
  }

  if (!config || !tools) {
    return <div className="text-sm text-muted-foreground">Loading configuration...</div>;
  }

  const handleSave = async () => {
    await patch({ tools });
  };

  const fmtBytes = (bytes: number) => {
    if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(0)} MiB`;
    if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(0)} KiB`;
    return `${bytes} B`;
  };

  return (
    <div className="space-y-6">
      {/* File Read Limits */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">File Read Limits</h3>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-end gap-2">
            <Input
              label="Max File Size"
              type="number"
              min={0}
              value={tools.maxFileSize}
              onChange={(e) => setTools({ ...tools, maxFileSize: Number(e.target.value) })}
              className="w-28 text-right"
            />
            <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.maxFileSize)})</span>
          </div>
          <Input
            label="Max Matches"
            type="number"
            min={0}
            value={tools.maxMatches}
            onChange={(e) => setTools({ ...tools, maxMatches: Number(e.target.value) })}
          />
          <Input
            label="Max Line Length"
            type="number"
            min={0}
            value={tools.maxLineLen}
            onChange={(e) => setTools({ ...tools, maxLineLen: Number(e.target.value) })}
          />
          <Input
            label="Max Glob Items"
            type="number"
            min={0}
            value={tools.maxGlobItems}
            onChange={(e) => setTools({ ...tools, maxGlobItems: Number(e.target.value) })}
          />
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Read Limits'}
          </Button>
        </div>
      </div>

      {/* File Write Limits */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">File Write Limits</h3>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-end gap-2">
            <Input
              label="Max Write Size"
              type="number"
              min={0}
              value={tools.maxWriteSize}
              onChange={(e) => setTools({ ...tools, maxWriteSize: Number(e.target.value) })}
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
              onChange={(e) => setTools({ ...tools, maxMultiWriteBytes: Number(e.target.value) })}
              className="w-28 text-right"
            />
            <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.maxMultiWriteBytes)})</span>
          </div>
          <Input
            label="Max Multi-Write Files"
            type="number"
            min={0}
            value={tools.maxMultiWriteFiles}
            onChange={(e) => setTools({ ...tools, maxMultiWriteFiles: Number(e.target.value) })}
          />
          <Input
            label="Max Replace Edits"
            type="number"
            min={0}
            value={tools.maxReplaceEdits}
            onChange={(e) => setTools({ ...tools, maxReplaceEdits: Number(e.target.value) })}
          />
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Write Limits'}
          </Button>
        </div>
      </div>

      {/* HTTP Limits */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">HTTP Limits</h3>
        <div className="grid gap-4">
          <div className="flex items-center justify-between">
            <div className="flex flex-col gap-1">
              <Label>Block Private IPs</Label>
              <span className="text-[10px] text-muted-foreground">Prevent requests to private/local network addresses</span>
            </div>
            <Switch checked={tools.httpBlockPrivate} onCheckedChange={(v) => setTools({ ...tools, httpBlockPrivate: v })} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="flex items-end gap-2">
              <Input
                label="Max Body Size"
                type="number"
                min={0}
                value={tools.httpMaxBody}
                onChange={(e) => setTools({ ...tools, httpMaxBody: Number(e.target.value) })}
                className="w-28 text-right"
              />
              <span className="pb-1 text-xs text-muted-foreground">bytes ({fmtBytes(tools.httpMaxBody)})</span>
            </div>
            <Input
              label="Timeout (ms)"
              type="number"
              min={0}
              value={tools.httpTimeoutMs}
              onChange={(e) => setTools({ ...tools, httpTimeoutMs: Number(e.target.value) })}
            />
          </div>
          <div>
            <Label className="mb-2 block">Allowed Hosts</Label>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {(tools.httpAllowedHosts || []).map((host, i) => (
                <span key={i} className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs">
                  {host}
                  <button onClick={() => setTools({ ...tools, httpAllowedHosts: tools.httpAllowedHosts?.filter((_, j) => j !== i) })}>
                    <X className="h-2.5 w-2.5 text-muted-foreground hover:text-foreground" />
                  </button>
                </span>
              ))}
            </div>
            <div className="flex gap-2">
              <Input placeholder="e.g. api.example.com" value={newAllowedHost} onChange={(e) => setNewAllowedHost(e.target.value)} className="flex-1" />
              <Button size="sm" variant="outline" onClick={() => {
                if (newAllowedHost.trim()) {
                  setTools({ ...tools, httpAllowedHosts: [...(tools.httpAllowedHosts || []), newAllowedHost.trim()] });
                  setNewAllowedHost('');
                }
              }}>
                <Plus className="h-3 w-3" />
              </Button>
            </div>
          </div>
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save HTTP Limits'}
          </Button>
        </div>
      </div>

      {/* Shell Limits */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Shell Limits</h3>
        <div className="grid gap-4">
          <div className="flex items-end gap-2">
            <Input
              label="Max Output"
              type="number"
              min={0}
              value={tools.shellMaxOutput}
              onChange={(e) => setTools({ ...tools, shellMaxOutput: Number(e.target.value) })}
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
                  <button onClick={() => setTools({ ...tools, shellBlockRegexes: tools.shellBlockRegexes?.filter((_, j) => j !== i) })}>
                    <X className="h-2.5 w-2.5 hover:text-foreground" />
                  </button>
                </span>
              ))}
            </div>
            <div className="flex gap-2">
              <Input placeholder="e.g. ^\\s*rm\\b" value={newBlockRegex} onChange={(e) => setNewBlockRegex(e.target.value)} className="flex-1 font-mono" />
              <Button size="sm" variant="outline" onClick={() => {
                if (newBlockRegex.trim()) {
                  setTools({ ...tools, shellBlockRegexes: [...(tools.shellBlockRegexes || []), newBlockRegex.trim()] });
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
                  <button onClick={() => setTools({ ...tools, shellConfirmRegexes: tools.shellConfirmRegexes?.filter((_, j) => j !== i) })}>
                    <X className="h-2.5 w-2.5 hover:text-foreground" />
                  </button>
                </span>
              ))}
            </div>
            <div className="flex gap-2">
              <Input placeholder="e.g. ^\\s*rm\\b" value={newConfirmRegex} onChange={(e) => setNewConfirmRegex(e.target.value)} className="flex-1 font-mono" />
              <Button size="sm" variant="outline" onClick={() => {
                if (newConfirmRegex.trim()) {
                  setTools({ ...tools, shellConfirmRegexes: [...(tools.shellConfirmRegexes || []), newConfirmRegex.trim()] });
                  setNewConfirmRegex('');
                }
              }}>
                <Plus className="h-3 w-3" />
              </Button>
            </div>
          </div>
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save Shell Limits'}
          </Button>
        </div>
      </div>

      {/* WebSearch */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Web Search</h3>
        <Input
          label="Timeout (ms)"
          type="number"
          min={0}
          value={tools.webSearchTimeoutMs}
          onChange={(e) => setTools({ ...tools, webSearchTimeoutMs: Number(e.target.value) })}
          className="w-40"
        />
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save WebSearch'}
          </Button>
          {error && <span className="text-[10px] text-destructive">{error}</span>}
        </div>
      </div>
    </div>
  );
}
