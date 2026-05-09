import { useConfig } from '@/hooks/useConfig';

export function GeneralTab() {
  const config = useConfig();

  if (!config) {
    return (
      <div className="text-sm text-muted-foreground">Loading configuration...</div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Session Config */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Session</h3>
        <div className="grid gap-3 text-sm">
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Timeline Max File Size</span>
            <span className="font-medium text-foreground">{config.session.timelineMaxFileMB} MB</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Timeline Max Files</span>
            <span className="font-medium text-foreground">{config.session.timelineMaxFiles}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Context Idle Threshold</span>
            <span className="font-medium text-foreground">{config.session.contextIdleThresholdMin} min</span>
          </div>
        </div>
      </div>

      {/* Log Config */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Logging</h3>
        <div className="grid gap-3 text-sm">
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Level</span>
            <span className="font-medium text-foreground">{config.log.level}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Console Output</span>
            <span className="font-medium text-foreground">{config.log.console ? 'Enabled' : 'Disabled'}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">File Output</span>
            <span className="font-medium text-foreground">{config.log.file ? 'Enabled' : 'Disabled'}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
