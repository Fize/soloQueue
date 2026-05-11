import { useState, useEffect, useCallback } from 'react';
import { getAgentProfile, updateAgentProfile } from '@/lib/api';
import type { AgentProfile } from '@/types';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Save, Heart, Scale, Eye, Pencil, Loader2 } from 'lucide-react';

// ─── Editor Section ────────────────────────────────────────────────────────

interface EditorSectionProps {
  title: string;
  icon: typeof Heart;
  content: string;
  onSave: (content: string) => Promise<void>;
  saving: boolean;
}

function EditorSection({ title, icon: Icon, content, onSave, saving }: EditorSectionProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(content);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Sync draft when content changes externally (e.g. after save)
  useEffect(() => {
    setDraft(content);
  }, [content]);

  const lineCount = draft.split('\n').length;
  const charCount = draft.length;

  const handleSave = async () => {
    setSaveError(null);
    try {
      await onSave(draft);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Save failed');
    }
  };

  const handleCancel = () => {
    setDraft(content);
    setEditing(false);
    setSaveError(null);
  };

  return (
    <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
      {/* Header */}
      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Icon className="h-4 w-4 text-foreground" />
          <h3 className="text-sm font-bold text-foreground">{title}</h3>
          <Badge variant="secondary" className="text-[10px]">
            {lineCount} lines · {charCount} chars
          </Badge>
        </div>

        {/* Edit / Preview toggle */}
        <div className="flex items-center gap-1">
          <Button
            size="sm"
            variant={editing ? 'outline' : 'default'}
            className="h-7 gap-1 text-xs"
            onClick={() => setEditing(false)}
            disabled={!editing}
          >
            <Eye className="h-3 w-3" />
            Preview
          </Button>
          <Button
            size="sm"
            variant={editing ? 'default' : 'outline'}
            className="h-7 gap-1 text-xs"
            onClick={() => setEditing(true)}
            disabled={editing}
          >
            <Pencil className="h-3 w-3" />
            Edit
          </Button>
        </div>
      </div>

      {/* Content area */}
      {editing ? (
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          className="w-full min-h-[400px] resize-y rounded-md border-2 border-border bg-[#1E1E2E] p-4 font-mono text-xs leading-relaxed text-[#E5E7EB] focus:outline-none focus:ring-2 focus:ring-primary/50"
          spellCheck={false}
        />
      ) : (
        <ScrollArea className="h-[400px] rounded-md border-2 border-border bg-[#1E1E2E] p-4">
          {content ? (
            <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed text-[#E5E7EB]">
              {content}
            </pre>
          ) : (
            <p className="text-sm text-muted-foreground">No content</p>
          )}
        </ScrollArea>
      )}

      {/* Footer */}
      {editing && (
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSave} disabled={saving || draft === content}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" /> Saving...
              </>
            ) : (
              <>
                <Save className="mr-1 h-3 w-3" /> Save {title}
              </>
            )}
          </Button>
          <Button size="sm" variant="outline" onClick={handleCancel} disabled={saving}>
            Cancel
          </Button>
          {saveError && <span className="text-[10px] text-destructive">{saveError}</span>}
        </div>
      )}
    </div>
  );
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function ProfileTab() {
  const [profile, setProfile] = useState<AgentProfile | null>(null);
  const [loading, setLoading] = useState(true);
  const [savingSoul, setSavingSoul] = useState(false);
  const [savingRules, setSavingRules] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchProfile = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await getAgentProfile('main');
      setProfile(data);
    } catch {
      setError('Failed to load profile');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchProfile();
  }, [fetchProfile]);

  const handleSaveSoul = async (soul: string) => {
    setSavingSoul(true);
    try {
      const updated = await updateAgentProfile('main', { soul });
      setProfile(updated);
    } finally {
      setSavingSoul(false);
    }
  };

  const handleSaveRules = async (rules: string) => {
    setSavingRules(true);
    try {
      const updated = await updateAgentProfile('main', { rules });
      setProfile(updated);
    } finally {
      setSavingRules(false);
    }
  };

  if (loading) {
    return <div className="text-sm text-muted-foreground">Loading profile...</div>;
  }

  if (error) {
    return <div className="text-sm text-destructive">{error}</div>;
  }

  if (!profile) {
    return <div className="text-sm text-muted-foreground">No profile available</div>;
  }

  return (
    <div className="space-y-6">
      <EditorSection
        title="Soul"
        icon={Heart}
        content={profile.soul}
        onSave={handleSaveSoul}
        saving={savingSoul}
      />
      <EditorSection
        title="Rules"
        icon={Scale}
        content={profile.rules}
        onSave={handleSaveRules}
        saving={savingRules}
      />
    </div>
  );
}
