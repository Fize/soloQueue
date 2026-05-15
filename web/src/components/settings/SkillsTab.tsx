import { useSkills } from '@/hooks/useToolsAndSkills';
import { Sparkles, BookOpen } from 'lucide-react';
import type { SkillInfo } from '@/types';

// ─── Skill Card ─────────────────────────────────────────────────────────────

function SkillCard({ skill }: { skill: SkillInfo }) {
  const isBuiltin = skill.category === 'builtin';

  return (
    <div className="nb-border rounded-lg bg-card p-4 nb-shadow-xs nb-card-hover">
      <div className="flex items-start gap-3">
        <div className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${isBuiltin ? 'bg-primary' : 'bg-success'}`}>
          <Sparkles className={`h-4 w-4 ${isBuiltin ? 'text-primary-foreground' : 'text-success-foreground'}`} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <code className="text-xs font-bold text-foreground">{skill.id}</code>
            <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${isBuiltin ? 'bg-primary text-primary-foreground' : 'bg-success text-success-foreground'}`}>
              {isBuiltin ? 'Built-in' : 'User'}
            </span>
            {!skill.user_invocable && (
              <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                AI only
              </span>
            )}
            {skill.context === 'fork' && (
              <span className="rounded bg-accent px-1.5 py-0.5 text-[10px] text-accent-foreground">
                fork
              </span>
            )}
          </div>
          <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
            {skill.description || 'No description'}
          </p>
          {skill.when_to_use && (
            <p className="mt-0.5 text-[10px] italic text-muted-foreground/70 leading-relaxed line-clamp-2">
              When: {skill.when_to_use}
            </p>
          )}
          {skill.allowed_tools && skill.allowed_tools.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1">
              {skill.allowed_tools.map((t, i) => (
                <span key={i} className="rounded bg-info/50 px-1.5 py-0.5 font-mono text-[9px] text-info-foreground">
                  {t}
                </span>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function SkillsTab() {
  const { skills, loading } = useSkills();

  if (loading) {
    return <div className="text-sm text-muted-foreground">Loading skills...</div>;
  }

  const skillList = skills?.skills ?? [];
  const builtinSkills = skillList.filter((s) => s.category === 'builtin');
  const userSkills = skillList.filter((s) => s.category === 'user');

  if (skillList.length === 0) {
    return (
      <div className="nb-border rounded-lg bg-card p-8 nb-shadow-sm text-center">
        <p className="text-sm text-muted-foreground">No skills registered</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Built-in Skills */}
      {builtinSkills.length > 0 && (
        <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
          <div className="mb-4 flex items-center gap-2">
            <Sparkles className="h-4 w-4 text-foreground" />
            <h3 className="text-sm font-bold text-foreground">Built-in Skills</h3>
            <span className="rounded bg-primary px-1.5 py-0.5 text-[10px] text-primary-foreground">
              {builtinSkills.length}
            </span>
          </div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {builtinSkills.map((s) => (
              <SkillCard key={s.id} skill={s} />
            ))}
          </div>
        </div>
      )}

      {/* User Skills */}
      {userSkills.length > 0 && (
        <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
          <div className="mb-4 flex items-center gap-2">
            <BookOpen className="h-4 w-4 text-foreground" />
            <h3 className="text-sm font-bold text-foreground">User Skills</h3>
            <span className="rounded bg-success px-1.5 py-0.5 text-[10px] text-success-foreground">
              {userSkills.length}
            </span>
          </div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {userSkills.map((s) => (
              <SkillCard key={s.id} skill={s} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
