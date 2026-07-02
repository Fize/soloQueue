import { useState, useEffect, useCallback, useRef } from "react";
import type { ChangesResponse, FileChange, DiffLine } from "@/types";
import { getSessionChanges } from "@/lib/api";
import {
  Loader2,
  FilePlus,
  FileMinus,
  FileEdit,
  RefreshCw,
  ChevronRight,
  Binary,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface SessionChangesPanelProps {
  sessionId: string;
}

function StatusIcon({ status }: { status: FileChange["status"] }) {
  if (status === "added")
    return <FilePlus className="h-3.5 w-3.5 text-emerald-500 shrink-0" />;
  if (status === "deleted")
    return <FileMinus className="h-3.5 w-3.5 text-red-500 shrink-0" />;
  return <FileEdit className="h-3.5 w-3.5 text-amber-500 shrink-0" />;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function DiffLineView({ line }: { line: DiffLine }) {
  const isAdd = line.type === "add";
  const isDel = line.type === "del";
  const isCtx = line.type === "ctx";

  return (
    <div
      className={cn(
        "flex font-mono text-[11px] leading-[1.5] whitespace-pre overflow-hidden",
        isAdd && "bg-emerald-500/10",
        isDel && "bg-red-500/10",
        isCtx && "bg-transparent",
      )}
    >
      <span className="select-none text-muted-foreground/40 w-10 text-right pr-2 shrink-0 tabular-nums">
        {line.old_num || ""}
      </span>
      <span className="select-none text-muted-foreground/40 w-10 text-right pr-2 shrink-0 tabular-nums border-r border-border/20">
        {line.new_num || ""}
      </span>
      <span
        className={cn(
          "select-none w-5 text-center shrink-0 font-bold",
          isAdd && "text-emerald-600",
          isDel && "text-red-600",
          isCtx && "text-muted-foreground/30",
        )}
      >
        {isAdd ? "+" : isDel ? "-" : " "}
      </span>
      <span
        className={cn(
          "flex-1 px-1 overflow-hidden",
          isAdd && "text-emerald-700 dark:text-emerald-400",
          isDel && "text-red-700 dark:text-red-400",
          isCtx && "text-foreground/70",
        )}
      >
        {line.content || " "}
      </span>
    </div>
  );
}

function FileDiffView({ change }: { change: FileChange }) {
  if (change.binary) {
    return (
      <div className="flex items-center gap-2 px-4 py-6 text-xs text-muted-foreground">
        <Binary className="h-4 w-4" />
        <span>Binary file changed ({formatBytes(change.size_bytes || 0)})</span>
      </div>
    );
  }

  if (!change.hunks || change.hunks.length === 0) {
    return (
      <div className="px-4 py-3 text-xs text-muted-foreground">
        {change.status === "deleted"
          ? "File deleted — no content to display."
          : "No diff available."}
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      {change.hunks.map((hunk, hi) => (
        <div key={hi} className="border-b border-border/20 last:border-0">
          <div className="px-4 py-1 bg-muted/30 text-[11px] font-mono text-muted-foreground/60 select-none">
            @@ -{hunk.old_start},{hunk.old_lines} +{hunk.new_start},
            {hunk.new_lines} @@
          </div>
          {hunk.lines.map((line, li) => (
            <DiffLineView key={li} line={line} />
          ))}
        </div>
      ))}
    </div>
  );
}

export function SessionChangesPanel({ sessionId }: SessionChangesPanelProps) {
  const [data, setData] = useState<ChangesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [expandedFile, setExpandedFile] = useState<string | null>(null);
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const isVisibleRef = useRef(true);

  const fetchChanges = useCallback(
    async (showSpinner = true) => {
      if (showSpinner) setLoading(true);
      setError("");
      try {
        const result = await getSessionChanges(sessionId);
        setData(result);
        if (result.changes.length > 0 && !expandedFile) {
          setExpandedFile(result.changes[0].path);
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : "Failed to load changes");
      } finally {
        if (showSpinner) setLoading(false);
      }
    },
    [sessionId, expandedFile],
  );

  useEffect(() => {
    fetchChanges();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  // Auto-refresh: poll every 5s, only when tab is visible.
  useEffect(() => {
    if (!sessionId) return;

    const startPolling = () => {
      if (pollingRef.current) return;
      isVisibleRef.current = document.visibilityState === "visible";
      pollingRef.current = setInterval(() => {
        if (!isVisibleRef.current) return;
        getSessionChanges(sessionId)
          .then(setData)
          .catch(() => {});
      }, 5000);
    };

    const stopPolling = () => {
      if (pollingRef.current) {
        clearInterval(pollingRef.current);
        pollingRef.current = null;
      }
    };

    const handleVisibility = () => {
      isVisibleRef.current = document.visibilityState === "visible";
      if (isVisibleRef.current) {
        // Fetch immediately when tab becomes visible.
        getSessionChanges(sessionId)
          .then(setData)
          .catch(() => {});
        startPolling();
      } else {
        stopPolling();
      }
    };

    document.addEventListener("visibilitychange", handleVisibility);
    startPolling();

    return () => {
      stopPolling();
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, [sessionId]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-6">
        <p className="text-xs text-destructive">{error}</p>
        <button
          onClick={() => fetchChanges(true)}
          className="min-h-[44px] min-w-[44px] flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
        >
          <RefreshCw className="h-3 w-3" />
          Retry
        </button>
      </div>
    );
  }

  if (!data || data.changes.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-6">
        <p className="text-xs text-muted-foreground">
          No changes in this session
        </p>
        <p className="text-[11px] text-muted-foreground/50">
          {data?.is_git_repo
            ? `Tracking from ${data.base_ref?.slice(0, 8)}`
            : "File changes will appear here"}
        </p>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col bg-card/10">
      {/* Header — uses background hierarchy instead of shadow */}
      <div className="shrink-0 px-4 py-3 border-b border-border/40 bg-card/40 backdrop-blur-md flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold tracking-wide text-foreground/90">
            Changes
          </h2>
          <span className="text-[11px] text-muted-foreground/60">
            {data.changes.length} {data.changes.length === 1 ? "file" : "files"}
          </span>
        </div>
        <div className="flex items-center gap-3 text-[11px] font-mono">
          <span className="text-emerald-600 dark:text-emerald-400">
            +{data.total_additions}
          </span>
          <span className="text-red-600 dark:text-red-400">
            -{data.total_deletions}
          </span>
          <button
            onClick={() => fetchChanges(false)}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center rounded hover:bg-foreground/10 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            title="Refresh"
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* File list + diff */}
      <div className="flex-1 min-h-0 overflow-y-auto">
        {data.changes.map((change) => {
          const isExpanded = expandedFile === change.path;
          return (
            <div
              key={change.path}
              className="border-b border-border/20 last:border-0"
            >
              {/* File header — HIG min 44pt touch target */}
              <button
                onClick={() => setExpandedFile(isExpanded ? null : change.path)}
                className="w-full min-h-[44px] flex items-center gap-2 px-4 hover:bg-foreground/8 transition-colors duration-200 text-left cursor-pointer"
              >
                <ChevronRight
                  className={cn(
                    "h-3.5 w-3.5 text-muted-foreground/40 shrink-0 transition-transform duration-250",
                    isExpanded && "rotate-90",
                  )}
                />
                <StatusIcon status={change.status} />
                <span className="font-mono text-[11px] text-foreground/80 truncate flex-1">
                  {change.path}
                </span>
                {!change.binary && (
                  <span className="text-[11px] font-mono shrink-0">
                    <span className="text-emerald-600 dark:text-emerald-400">
                      +{change.additions}
                    </span>{" "}
                    <span className="text-red-600 dark:text-red-400">
                      -{change.deletions}
                    </span>
                  </span>
                )}
              </button>
              {/* Diff content — height-animated expand/collapse */}
              <div
                className={cn(
                  "grid transition-all duration-250 ease-in-out",
                  isExpanded
                    ? "grid-rows-[1fr] opacity-100"
                    : "grid-rows-[0fr] opacity-0",
                )}
              >
                <div className="overflow-hidden">
                  <div className="border-t border-border/10 bg-muted/5">
                    <FileDiffView change={change} />
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      </div>

      {/* Footer */}
      <div className="shrink-0 px-4 py-2 border-t border-border/40 bg-card/20 text-[11px] text-muted-foreground/50 font-mono truncate">
        {data.is_git_repo
          ? `base: ${data.base_ref?.slice(0, 12) || "unknown"}`
          : "snapshot-based (non-git)"}
      </div>
    </div>
  );
}
