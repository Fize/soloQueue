import { useConfig } from '@/hooks/useConfig';
import { Badge } from '@/components/ui/badge';

export function ModelsTab() {
  const config = useConfig();

  if (!config) {
    return (
      <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
        Loading configuration...
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Providers */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="mb-4 text-sm font-bold text-foreground">Providers</h3>
        <div className="space-y-2">
          {config.providers.map((provider) => (
            <div
              key={provider.id}
              className="flex items-center justify-between rounded-md border-2 border-[#EEEEEE] px-3 py-2"
            >
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-foreground">{provider.name}</span>
                {provider.isDefault && (
                  <Badge variant="default" className="text-[10px]">Default</Badge>
                )}
              </div>
              <Badge variant={provider.enabled ? 'secondary' : 'outline'} className="text-[10px]">
                {provider.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </div>
          ))}
        </div>
      </div>

      {/* Models */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="mb-4 text-sm font-bold text-foreground">Models</h3>
        <div className="space-y-2">
          {config.models.map((model) => (
            <div
              key={model.id}
              className="flex items-center justify-between rounded-md border-2 border-[#EEEEEE] px-3 py-2"
            >
              <div>
                <span className="text-sm font-medium text-foreground">{model.name}</span>
                <p className="text-[10px] text-muted-foreground">
                  Context: {model.contextWindow.toLocaleString()} | Provider: {model.providerId}
                </p>
              </div>
              <div className="flex items-center gap-1.5">
                {model.thinking.enabled && (
                  <Badge variant="default" className="text-[10px]">Thinking</Badge>
                )}
                <Badge variant={model.enabled ? 'secondary' : 'outline'} className="text-[10px]">
                  {model.enabled ? 'On' : 'Off'}
                </Badge>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Default Models */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="mb-4 text-sm font-bold text-foreground">Default Model Assignments</h3>
        <div className="space-y-3">
          {Object.entries(config.defaultModels).map(([role, modelId]) => (
            <div key={role} className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground capitalize">{role}</span>
              <span className="font-mono text-xs text-foreground">{modelId}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
