import { useEffect, useCallback, useLayoutEffect, useRef } from "react";
import type { FileChange, DiffLine } from "@/types";
import { getSessionChangeDiff, getSessionChanges } from "@/lib/api";
import { emptySessionChangesEntry, useSessionChangesStore } from "@/stores/sessionChangesStore";
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
import { useTranslation } from "@/lib/i18n";

interface SessionChangesPanelProps {
  sessionId: string;
}

function StatusIcon({ status }: { status: FileChange["status"] }) {
  if (status === "added")
    return <FilePlus className="h-3.5 w-3.5 text-success shrink-0" />;
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
        "flex font-mono text-[11px] leading-[1.5] whitespace-pre",
        isAdd && "bg-success/10",
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
          isAdd && "text-success",
          isDel && "text-red-600",
          isCtx && "text-muted-foreground/30",
        )}
      >
        {isAdd ? "+" : isDel ? "-" : " "}
      </span>
      <span
        className={cn(
          "flex-1 px-1",
          isAdd && "text-success",
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
  const storedEntry = useSessionChangesStore((state) => state.sessions[sessionId])
  const entry = storedEntry ?? emptySessionChangesEntry()
  const data = entry.summary
  const { loading, error, expandedFile } = entry
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const isVisibleRef = useRef(true)
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const { t } = useTranslation()
  const genericError = t('common.error')

  const fetchChanges = useCallback(
    async (showSpinner = true, silent = false) => {
      const generation = useSessionChangesStore.getState().beginSummaryRequest(sessionId)
      useSessionChangesStore.getState().setLoading(sessionId, showSpinner, generation)
      if (!silent) useSessionChangesStore.getState().setError(sessionId, '', generation)
      try {
        const result = await getSessionChanges(sessionId)
        result.changes = result.changes ?? []
        useSessionChangesStore.getState().setSummary(sessionId, result, generation)
      } catch (e) {
        if (!silent) {
          useSessionChangesStore
            .getState()
            .setError(sessionId, e instanceof Error ? e.message : genericError, generation)
        }
      } finally {
        useSessionChangesStore.getState().setLoading(sessionId, false, generation)
      }
    },
    [genericError, sessionId]
  )

  useEffect(() => {
    const hasCachedSummary = Boolean(useSessionChangesStore.getState().sessions[sessionId]?.summary)
    void fetchChanges(!hasCachedSummary, hasCachedSummary)
  }, [fetchChanges, sessionId])

  // Auto-refresh: poll every 5s, only when tab is visible.
  useEffect(() => {
    if (!sessionId) return

    const startPolling = () => {
      if (pollingRef.current) return
      isVisibleRef.current = document.visibilityState === 'visible'
      pollingRef.current = setInterval(() => {
        if (!isVisibleRef.current) return
        void fetchChanges(false, true)
      }, 5000)
    }

    const stopPolling = () => {
      if (pollingRef.current) {
        clearInterval(pollingRef.current)
        pollingRef.current = null
      }
    }

    const handleVisibility = () => {
      isVisibleRef.current = document.visibilityState === 'visible'
      if (isVisibleRef.current) {
        // Fetch immediately when tab becomes visible.
        void fetchChanges(false, true)
        startPolling()
      } else {
        stopPolling()
      }
    }

    document.addEventListener('visibilitychange', handleVisibility)
    startPolling()

    return () => {
      stopPolling()
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [fetchChanges, sessionId])

  useLayoutEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = entry.scrollTop
    }
  }, [entry.scrollTop, sessionId])

  const loadDiff = useCallback(
    async (change: FileChange, retry = false) => {
      const current = useSessionChangesStore.getState().sessions[sessionId]
      if (
        change.binary ||
        current?.diffLoading[change.path] ||
        (current?.diffErrors[change.path] && !retry) ||
        current?.diffs[change.path]?.revision === change.revision
      )
        return
      useSessionChangesStore.getState().setDiffLoading(sessionId, change.path, true)
      useSessionChangesStore.getState().setDiffError(sessionId, change.path, '')
      try {
        const diff = await getSessionChangeDiff(sessionId, change.path, change.revision)
        const latest = useSessionChangesStore
          .getState()
          .sessions[sessionId]?.summary?.changes.find((item) => item.path === change.path)
        if (latest?.revision === diff.revision) {
          useSessionChangesStore.getState().setDiff(sessionId, diff)
        }
      } catch (e) {
        const latest = useSessionChangesStore
          .getState()
          .sessions[sessionId]?.summary?.changes.find((item) => item.path === change.path)
        if (latest?.revision === change.revision) {
          useSessionChangesStore
            .getState()
            .setDiffError(sessionId, change.path, e instanceof Error ? e.message : genericError)
        }
      } finally {
        useSessionChangesStore.getState().setDiffLoading(sessionId, change.path, false)
      }
    },
    [genericError, sessionId]
  )

  useEffect(() => {
    if (!expandedFile) return
    const change = data?.changes.find((item) => item.path === expandedFile)
    if (change) void loadDiff(change)
  }, [data, entry.diffLoading, expandedFile, loadDiff])

  const toggleFile = (change: FileChange) => {
    if (expandedFile === change.path) {
      useSessionChangesStore.getState().setExpandedFile(sessionId, null)
      return
    }
    useSessionChangesStore.getState().setExpandedFile(sessionId, change.path)
    void loadDiff(change)
  }

  return (
    <div className="flex h-full flex-col bg-card/10">
      {/* Header — always visible */}
      <div className="shrink-0 px-4 py-3 border-b border-border/40 bg-card/40 backdrop-blur-md flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold tracking-wide text-foreground/90">
            {t('chat.changes')}
          </h2>
          {data && !loading && !error && (
            <span className="text-[11px] text-muted-foreground/60">
              {data.changes.length} {data.changes.length === 1 ? t('chat.files') : t('chat.files')}
            </span>
          )}
        </div>
        {!loading && (
          <div className="flex items-center gap-3 text-[11px] font-mono">
            {data && !error && (
              <>
                <span className="text-success">+{data.total_additions}</span>
                <span className="text-red-600 dark:text-red-400">-{data.total_deletions}</span>
              </>
            )}
            <button
              onClick={() => fetchChanges(false)}
              className="min-h-[44px] min-w-[44px] flex items-center justify-center rounded hover:bg-foreground/10 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
              title={t('common.refresh')}
            >
              <RefreshCw className="h-3.5 w-3.5" />
            </button>
          </div>
        )}
      </div>

      {/* Main Content Area */}
      <div
        ref={scrollRef}
        data-testid="changes-scroll"
        onScroll={(event) =>
          useSessionChangesStore.getState().setScrollTop(sessionId, event.currentTarget.scrollTop)
        }
        className="flex-1 min-h-0 overflow-y-auto"
      >
        {loading ? (
          <div className="flex h-full items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : error ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
            <p className="text-xs text-destructive max-w-xs break-all">{error}</p>
            <button
              onClick={() => fetchChanges(true)}
              className="min-h-[44px] min-w-[44px] flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            >
              <RefreshCw className="h-3 w-3" />
              {t('chat.retry')}
            </button>
          </div>
        ) : !data || data.changes.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 p-6">
            <p className="text-xs text-muted-foreground">{t('chat.noChanges')}</p>
            <p className="text-[11px] text-muted-foreground/50">
              {data?.is_git_repo
                ? t('chat.gitTracking', { ref: data.base_ref?.slice(0, 8) ?? '' })
                : t('chat.fileChangesAppearHere')}
            </p>
          </div>
        ) : (
          /* File list + diff */
          data.changes.map((change) => {
            const isExpanded = expandedFile === change.path
            return (
              <div key={change.path} className="border-b border-border/20 last:border-0">
                {/* File header — HIG min 44pt touch target */}
                <button
                  onClick={() => toggleFile(change)}
                  className="w-full min-h-[44px] flex items-center gap-2 px-4 hover:bg-foreground/8 transition-colors duration-200 text-left cursor-pointer"
                >
                  <ChevronRight
                    className={cn(
                      'h-3.5 w-3.5 text-muted-foreground/40 shrink-0 transition-transform duration-250',
                      isExpanded && 'rotate-90'
                    )}
                  />
                  <StatusIcon status={change.status} />
                  <span className="font-mono text-[11px] text-foreground/80 truncate flex-1">
                    {change.path}
                  </span>
                  {!change.binary && (
                    <span className="text-[11px] font-mono shrink-0">
                      <span className="text-success">+{change.additions}</span>{' '}
                      <span className="text-red-600 dark:text-red-400">-{change.deletions}</span>
                    </span>
                  )}
                </button>
                {/* Diff content — height-animated expand/collapse */}
                <div
                  className={cn(
                    'grid transition-all duration-250 ease-in-out',
                    isExpanded ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0'
                  )}
                >
                  <div className="overflow-hidden">
                    <div className="border-t border-border/10 bg-muted/5">
                      {entry.diffLoading[change.path] ? (
                        <div className="flex justify-center px-4 py-6">
                          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        </div>
                      ) : entry.diffErrors[change.path] ? (
                        <div className="flex items-center justify-between gap-3 px-4 py-3 text-xs text-destructive">
                          <span>{entry.diffErrors[change.path]}</span>
                          <button
                            onClick={() => loadDiff(change, true)}
                            className="text-muted-foreground hover:text-foreground cursor-pointer"
                          >
                            {t('chat.retry')}
                          </button>
                        </div>
                      ) : (
                        <FileDiffView change={entry.diffs[change.path] ?? change} />
                      )}
                    </div>
                  </div>
                </div>
              </div>
            )
          })
        )}
      </div>

      {/* Footer */}
      {data && !loading && !error && (
        <div className="shrink-0 px-4 py-2 border-t border-border/40 bg-card/20 text-[11px] text-muted-foreground/50 font-mono truncate">
          {data.is_git_repo
            ? t('chat.baseRef', { ref: data.base_ref?.slice(0, 12) || 'unknown' })
            : t('chat.nonGitSnapshot')}
        </div>
      )}
    </div>
  )
}
