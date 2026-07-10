import { SessionFilePanel } from '@/components/SessionFilePanel'
import { SessionPlanPanel } from '@/components/SessionPlanPanel'
import { SessionChangesPanel } from '@/components/SessionChangesPanel'

export function SessionInspectorPanel({
  activeSession,
  inspectorTab,
  panelWidth,
  projectPathFallback,
}: {
  activeSession: any
  inspectorTab: 'files' | 'changes' | 'plan'
  panelWidth: number
  projectPathFallback?: string
}) {
  if (!activeSession) return null

  return (
    <div className="h-full flex flex-col">
      {/* Panel Body */}
      <div className="flex-1 min-h-0 overflow-hidden">
        {inspectorTab === 'files' ? (
          activeSession.project_path || projectPathFallback ? (
            <SessionFilePanel
              projectPath={activeSession.project_path || projectPathFallback!}
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
