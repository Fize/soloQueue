import { SessionFilePanel } from '@/components/SessionFilePanel'
import { SessionPlanPanel } from '@/components/SessionPlanPanel'
import { SessionChangesPanel } from '@/components/SessionChangesPanel'
import { useTranslation } from '@/lib/i18n'

export function SessionInspectorPanel({
  activeSession,
  inspectorTab,
  panelWidth,
  projectPathFallback,
  selectedPlan,
  onSelectedPlanChange,
}: {
  activeSession: any
  inspectorTab: 'files' | 'changes' | 'plan'
  panelWidth: number
  projectPathFallback?: string
  selectedPlan?: string
  onSelectedPlanChange?: (path: string) => void
}) {
  const { t } = useTranslation()

  if (!activeSession) return null

  return (
    <div className="h-full flex flex-col">
      {/* Panel Body */}
      <div className="flex-1 min-h-0 overflow-hidden">
        {inspectorTab === 'files' ? (
          activeSession.project_path || projectPathFallback ? (
            <SessionFilePanel
              key={`${activeSession.id}:${activeSession.project_path || projectPathFallback!}`}
              sessionId={activeSession.id}
              projectPath={activeSession.project_path || projectPathFallback!}
              panelWidth={panelWidth}
            />
          ) : (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
              {t('chat.noProject')}
            </div>
          )
        ) : inspectorTab === 'plan' ? (
          <SessionPlanPanel
            key={activeSession.id}
            plans={activeSession.plans || []}
            selectedPlan={selectedPlan}
            onSelectedPlanChange={onSelectedPlanChange}
          />
        ) : (
          <SessionChangesPanel sessionId={activeSession.id} />
        )}
      </div>
    </div>
  )
}
