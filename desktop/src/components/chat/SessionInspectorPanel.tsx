import { FolderOpen, Layers, FileText } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SessionFilePanel } from '@/components/SessionFilePanel'
import { SessionPlanPanel } from '@/components/SessionPlanPanel'
import { SessionChangesPanel } from '@/components/SessionChangesPanel'

export function SessionInspectorPanel({
  activeSession,
  inspectorTab,
  setInspectorTab,
  panelWidth,
}: {
  activeSession: any
  inspectorTab: 'files' | 'changes' | 'plan'
  setInspectorTab: (tab: 'files' | 'changes' | 'plan') => void
  panelWidth: number
}) {
  if (!activeSession) return null

  return (
    <div className="h-full flex flex-col">
      {/* Tab Switcher */}
      <div className="flex items-center gap-1 shrink-0 h-10 px-3 border-b border-border/30 bg-card/20">
        <button
          onClick={() => setInspectorTab('files')}
          className={cn(
            'flex shrink-0 items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium transition-colors cursor-pointer',
            inspectorTab === 'files'
              ? 'bg-primary/10 text-primary'
              : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
          )}
        >
          <FolderOpen className="h-3.5 w-3.5" />
          Files
        </button>
        <button
          onClick={() => setInspectorTab('changes')}
          className={cn(
            'flex shrink-0 items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium transition-colors cursor-pointer',
            inspectorTab === 'changes'
              ? 'bg-primary/10 text-primary'
              : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
          )}
        >
          <Layers className="h-3.5 w-3.5" />
          Changes
        </button>
        <button
          onClick={() => setInspectorTab('plan')}
          className={cn(
            'flex shrink-0 items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium transition-colors cursor-pointer',
            inspectorTab === 'plan'
              ? 'bg-primary/10 text-primary'
              : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
          )}
        >
          <FileText className="h-3.5 w-3.5" />
          Plans
        </button>
      </div>

      {/* Panel Body */}
      <div className="flex-1 min-h-0 overflow-hidden">
        {inspectorTab === 'files' ? (
          activeSession.project_path ? (
            <SessionFilePanel
              projectPath={activeSession.project_path}
              panelWidth={panelWidth}
            />
          ) : (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
              Current session not associated with a project
            </div>
          )
        ) : inspectorTab === 'plan' ? (
          <SessionPlanPanel plans={activeSession.plans || []} />
        ) : (
          <SessionChangesPanel sessionId={activeSession.id} />
        )}
      </div>
    </div>
  )
}
