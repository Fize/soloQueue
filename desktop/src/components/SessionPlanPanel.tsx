import { useEffect, useState, useCallback } from "react";
import { Loader2, FileText } from "lucide-react";
import { getFileUrl, toggleFileCheckbox } from "@/lib/api";
import { MarkdownPreview } from "@/components/ui/markdown-preview";
import { cn } from "@/lib/utils";

interface SessionPlanPanelProps {
  plans: string[];
}

export function SessionPlanPanel({ plans }: SessionPlanPanelProps) {
  const [selectedPlan, setSelectedPlan] = useState<string>(plans[0] || "");
  const [content, setContent] = useState<string>("");
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string>("");

  // Sync selected plan if plans list changes and current selection is no longer in the list
  useEffect(() => {
    if (plans.length > 0 && !plans.includes(selectedPlan)) {
      setSelectedPlan(plans[0]);
    }
  }, [plans, selectedPlan]);

  const loadPlanContent = useCallback(async (path: string, showSpinner = true) => {
    if (showSpinner) setLoading(true);
    setError("");
    try {
      const url = getFileUrl(path);
      const res = await fetch(url);
      if (!res.ok) {
        throw new Error(`Failed to load plan file: ${res.statusText}`);
      }
      const text = await res.text();
      setContent(text);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load plan content");
    } finally {
      if (showSpinner) setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (selectedPlan) {
      loadPlanContent(selectedPlan, true);
    }
  }, [selectedPlan, loadPlanContent]);

  const handleToggleCheckbox = async (index: number) => {
    if (!selectedPlan) return;
    try {
      await toggleFileCheckbox(selectedPlan, index);
      // Reload content without showing the full page loader for smooth micro-interaction
      await loadPlanContent(selectedPlan, false);
    } catch (err) {
      console.error("Failed to toggle checkbox:", err);
    }
  };

  if (plans.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-muted-foreground p-6 text-center">
        No plans found for this session
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col bg-card/10">
      {/* Header */}
      <div className="shrink-0 px-4 py-3 border-b border-border/40 bg-card/40 backdrop-blur-md">
        <div className="flex items-center gap-2 mb-2">
          <FileText className="h-4 w-4 text-primary" />
          <h2 className="text-sm font-semibold tracking-wide text-foreground/90">
            Session Plans
          </h2>
          <span className="text-[11px] text-muted-foreground/60">
            {plans.length} {plans.length === 1 ? "plan" : "plans"}
          </span>
        </div>

        {/* Plan Selector tabs if there are multiple plans */}
        {plans.length > 1 && (
          <div className="flex flex-wrap gap-1.5 mt-2 pt-1 border-t border-border/10 max-h-24 overflow-y-auto">
            {plans.map((p) => {
              const parts = p.split(/[/\\]/);
              const basename = parts[parts.length - 1];
              const isSelected = p === selectedPlan;
              return (
                <button
                  key={p}
                  onClick={() => setSelectedPlan(p)}
                  className={cn(
                    "px-2 py-1 rounded text-[11px] font-medium transition-all cursor-pointer border",
                    isSelected
                      ? "bg-primary/10 text-primary border-primary/20 shadow-sm"
                      : "text-muted-foreground hover:text-foreground hover:bg-foreground/5 border-transparent"
                  )}
                  title={p}
                >
                  {basename}
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Main Content Area */}
      <div className="flex-1 min-h-0 overflow-y-auto p-4">
        {loading ? (
          <div className="flex h-full items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : error ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
            <p className="text-xs text-destructive max-w-xs break-all">{error}</p>
            <button
              onClick={() => loadPlanContent(selectedPlan, true)}
              className="px-3 py-1.5 rounded bg-primary/10 text-primary hover:bg-primary/20 text-xs font-medium transition-colors cursor-pointer"
            >
              Retry
            </button>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Display plan path */}
            <div className="p-2 rounded bg-muted/30 border border-border/20 text-[10px] font-mono text-muted-foreground/80 break-all select-all">
              Path: {selectedPlan}
            </div>

            {/* Render Plan Markdown */}
            <div className="prose dark:prose-invert prose-xs max-w-none">
              <MarkdownPreview
                content={content}
                basePath={selectedPlan}
                onToggleCheckbox={handleToggleCheckbox}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
