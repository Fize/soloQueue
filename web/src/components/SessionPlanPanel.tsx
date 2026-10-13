import { useEffect, useRef, useState, useCallback } from "react";
import { Loader2, FileText } from "lucide-react";
import { listFiles, readFile, toggleFileCheckbox } from "@/lib/api";
import type { FileInfo } from "@/types";
import { MarkdownPreview } from "@/components/ui/markdown-preview";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/lib/i18n";

interface SessionPlanPanelProps {
  plans: string[];
  selectedPlan?: string;
  onSelectedPlanChange?: (path: string) => void;
}

function parentDirectory(path: string) {
  const separator = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
  return separator <= 0 ? path.slice(0, Math.max(separator, 0) + 1) : path.slice(0, separator);
}

export function SessionPlanPanel({
  plans,
  selectedPlan,
  onSelectedPlanChange,
}: SessionPlanPanelProps) {
  const [orderedPlans, setOrderedPlans] = useState<string[]>([]);
  const [internalSelectedPlan, setInternalSelectedPlan] = useState<string>(selectedPlan || "");
  const [content, setContent] = useState<string>("");
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string>("");
  const [metadataReady, setMetadataReady] = useState(false);
  const { t } = useTranslation();
  const preferredPlan = selectedPlan ?? internalSelectedPlan;
  const effectiveSelectedPlan = orderedPlans.includes(preferredPlan)
    ? preferredPlan
    : (orderedPlans[0] || "");
  const planRequestRef = useRef(0);
  const metadataRequestRef = useRef(0);
  const metadataInitializedRef = useRef(false);
  const hasLoadedContentRef = useRef(false);
  const loadingPathRef = useRef<string | null>(null);
  const mountedRef = useRef(true);
  const selectedPlanRef = useRef(preferredPlan);
  const onSelectedPlanChangeRef = useRef(onSelectedPlanChange);

  useEffect(() => {
    selectedPlanRef.current = preferredPlan;
    onSelectedPlanChangeRef.current = onSelectedPlanChange;
  }, [onSelectedPlanChange, preferredPlan]);

  const selectPlan = useCallback((path: string) => {
    selectedPlanRef.current = path;
    setInternalSelectedPlan(path);
    onSelectedPlanChangeRef.current?.(path);
  }, []);

  const loadPlanContent = useCallback(async (path: string, showSpinner = true) => {
    if (!showSpinner && loadingPathRef.current === path) return;
    const requestId = ++planRequestRef.current;
    if (showSpinner) {
      loadingPathRef.current = path;
      setLoading(true);
    }
    if (showSpinner) setError("");
    try {
      const text = await readFile(path);
      if (!mountedRef.current || requestId !== planRequestRef.current) return;
      hasLoadedContentRef.current = true;
      setError("");
      setContent(text);
    } catch (err) {
      if (!mountedRef.current || requestId !== planRequestRef.current) return;
      if (showSpinner) {
        setError(err instanceof Error ? err.message : t('common.error'));
      }
    } finally {
      if (showSpinner && mountedRef.current && requestId === planRequestRef.current) {
        loadingPathRef.current = null;
        setLoading(false);
      }
    }
  }, [t]);

  useEffect(() => {
    const requestId = ++metadataRequestRef.current;
    if (plans.length === 0) {
      planRequestRef.current += 1;
      loadingPathRef.current = null;
      metadataInitializedRef.current = false;
      hasLoadedContentRef.current = false;
      // An empty update clears only Plan state; the empty-state UI is rendered below.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setOrderedPlans([]);
      setMetadataReady(false);
      setLoading(false);
      setError("");
      if (selectedPlanRef.current) selectPlan("");
      return;
    }

    const directories = [...new Set(plans.map(parentDirectory))];
    Promise.all(directories.map((directory) => listFiles(directory)))
      .then((groups) => {
        if (!mountedRef.current || requestId !== metadataRequestRef.current) return;
        const metadata = new Map<string, FileInfo>();
        groups.flat().forEach((entry) => metadata.set(entry.path, entry));
        const sortedPlans = plans
          .map((path, index) => ({ path, index, modTime: Date.parse(metadata.get(path)?.modTime || '') || 0 }))
          .sort((a, b) => b.modTime - a.modTime || a.index - b.index)
          .map(({ path }) => path);
        const currentPlan = selectedPlanRef.current;
        const nextPlan = sortedPlans.includes(currentPlan) ? currentPlan : sortedPlans[0];
        const wasInitialized = metadataInitializedRef.current;

        metadataInitializedRef.current = true;
        setOrderedPlans(sortedPlans);
        setMetadataReady(true);
        if (nextPlan !== currentPlan) {
          selectPlan(nextPlan);
        } else if (wasInitialized) {
          void loadPlanContent(nextPlan, !hasLoadedContentRef.current);
        }
      })
      .catch(() => {
        if (!mountedRef.current || requestId !== metadataRequestRef.current) return;
        const currentPlan = selectedPlanRef.current;
        const nextPlan = plans.includes(currentPlan) ? currentPlan : plans[0];
        const wasInitialized = metadataInitializedRef.current;
        metadataInitializedRef.current = true;
        setOrderedPlans(plans);
        setMetadataReady(true);
        if (nextPlan !== currentPlan) {
          selectPlan(nextPlan);
        } else if (wasInitialized) {
          void loadPlanContent(nextPlan, !hasLoadedContentRef.current);
        }
      });
  }, [plans, loadPlanContent, selectPlan]);

  useEffect(() => {
    if (!metadataReady || !effectiveSelectedPlan) {
      planRequestRef.current += 1
      return
    }
    // Plan content is loaded when the selected plan changes.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadPlanContent(effectiveSelectedPlan, true)
  }, [effectiveSelectedPlan, loadPlanContent, metadataReady]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      planRequestRef.current += 1;
      metadataRequestRef.current += 1;
      loadingPathRef.current = null;
    };
  }, []);

  const handleToggleCheckbox = async (index: number) => {
    if (!effectiveSelectedPlan) return;
    const requestId = planRequestRef.current;
    try {
      await toggleFileCheckbox(effectiveSelectedPlan, index);
      // A plan switch starts a newer request; do not reload the old plan.
      if (requestId !== planRequestRef.current) return;
      // Reload content without showing the full page loader for smooth micro-interaction
      await loadPlanContent(effectiveSelectedPlan, false);
    } catch (err) {
      console.error("Failed to toggle checkbox:", err);
    }
  };

  if (plans.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-muted-foreground p-6 text-center">
        {t('chat.noPlans')}
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
            {t('chat.sessionPlans')}
          </h2>
          <span className="text-[11px] text-muted-foreground/60">
            {t('chat.plansCount', { count: plans.length })}
          </span>
        </div>

        {/* Plan Selector tabs if there are multiple plans */}
        {orderedPlans.length > 1 && (
          <div className="flex flex-wrap gap-1.5 mt-2 pt-1 border-t border-border/10 max-h-24 overflow-y-auto">
            {orderedPlans.map((p) => {
              const parts = p.split(/[/\\]/);
              const basename = parts[parts.length - 1];
              const isSelected = p === effectiveSelectedPlan;
              return (
                <button
                  key={p}
                  onClick={() => selectPlan(p)}
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
              onClick={() => loadPlanContent(effectiveSelectedPlan, true)}
              className="px-3 py-1.5 rounded bg-primary/10 text-primary hover:bg-primary/20 text-xs font-medium transition-colors cursor-pointer"
            >
              {t('chat.retry')}
            </button>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Display plan path */}
            <div className="p-2 rounded bg-muted/30 border border-border/20 text-[10px] font-mono text-muted-foreground/80 break-all select-all">
              {t('chat.path')} {effectiveSelectedPlan}
            </div>

            {/* Render Plan Markdown */}
            <div className="prose dark:prose-invert prose-xs max-w-none">
              <MarkdownPreview
                content={content}
                basePath={effectiveSelectedPlan}
                onToggleCheckbox={handleToggleCheckbox}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
