import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  Activity,
  AlertCircle,
  BarChart3,
  Clock3,
  Database,
  FilterX,
  Gauge,
  Loader2,
  RefreshCw,
  Sparkles,
  Zap,
} from "lucide-react";

import { formatTokenCount } from "../App";
import { useTranslation } from "../i18n";
import {
  getStatsActivity,
  getStatsBreakdown,
  getStatsEvents,
  getStatsFilters,
  getStatsOverview,
  type StatsActivity,
  type StatsBreakdown,
  type StatsCoverageCount,
  type StatsDimension,
  type StatsEvent,
  type StatsFilterOption,
  type StatsFilters,
  type StatsMetrics,
  type StatsOverview,
  type StatsQuery,
} from "../lib/stats-api";

type RangeKey = "24h" | "7d" | "30d";
type TrendMetric = "tokens" | "calls" | "errors" | "latency";

interface DashboardFilters {
  team: string;
  origin: string;
  usageType: string;
  taskType: string;
  model: string;
  status: string;
}

const EMPTY_FILTERS: DashboardFilters = {
  team: "",
  origin: "",
  usageType: "",
  taskType: "",
  model: "",
  status: "",
};

const DIMENSIONS: StatsDimension[] = [
  "model",
  "usage_type",
  "team",
  "task_type",
  "origin",
  "status",
];

function rangeToQuery(range: RangeKey, now: number) {
  const days = range === "24h" ? 1 : range === "7d" ? 7 : 30;
  return {
    from: new Date(now - days * 86_400_000).toISOString(),
    to: new Date(now).toISOString(),
  };
}

function formatDelta(value: number | null | undefined) {
  if (value === null || value === undefined) return "—";
  return `${value > 0 ? "+" : ""}${value.toFixed(1)}%`;
}

function formatDuration(value: number) {
  return value >= 1000 ? `${(value / 1000).toFixed(1)} s` : `${value} ms`;
}

function metricValue(metrics: StatsMetrics, metric: TrendMetric) {
  if (metric === "calls") return metrics.request_count;
  if (metric === "errors") return metrics.error_count + metrics.timeout_count;
  if (metric === "latency") return metrics.p95_duration_ms;
  return metrics.total_tokens;
}

function pointLabel(value: string, bucket: string, timezone: string) {
  return new Intl.DateTimeFormat(undefined, {
    timeZone: timezone,
    month: "numeric",
    day: "numeric",
    ...(bucket === "hour" ? { hour: "2-digit" as const } : {}),
  }).format(new Date(value));
}

function Kpi({
  label,
  value,
  icon,
  delta,
  hint,
}: {
  label: string;
  value: string;
  icon: ReactNode;
  delta?: number | null;
  hint?: string;
}) {
  return (
    <article className="min-w-0 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface-secondary)] p-3 sm:p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted-foreground)]">
            {label}
          </p>
          <p className="mt-2 text-xl font-semibold tabular-nums text-[var(--color-foreground)] sm:text-2xl">
            {value}
          </p>
        </div>
        <span className="rounded-lg border border-[var(--color-border)] p-2 text-[var(--color-accent)]">
          {icon}
        </span>
      </div>
      <p className="mt-2 min-h-4 text-[10px] text-[var(--color-muted-foreground)]">
        {delta !== undefined ? `${formatDelta(delta)} ${hint ?? ""}` : hint}
      </p>
    </article>
  );
}

function SelectField({
  label,
  value,
  options,
  allLabel,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  options?: StatsFilterOption[];
  allLabel: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <label className="min-w-0 text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted-foreground)]">
      {label}
      <select
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 block min-h-10 w-full rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] px-3 text-xs text-[var(--color-foreground)] disabled:opacity-50"
      >
        <option value="">{allLabel}</option>
        {(options ?? []).map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}

function BreakdownCard({
  title,
  data,
  unavailable,
  emptyLabel,
  unavailableLabel,
  formatLabel = (_key, label) => label,
}: {
  title: string;
  data?: StatsBreakdown;
  unavailable: boolean;
  emptyLabel: string;
  unavailableLabel: string;
  formatLabel?: (key: string, label: string) => string;
}) {
  const maximum = Math.max(
    ...(data?.items.map((item) => item.metrics.total_tokens) ?? [0]),
    1,
  );
  return (
    <article className="rounded-xl border border-[var(--color-border)] p-4">
      <h3 className="text-xs font-semibold text-[var(--color-foreground)]">{title}</h3>
      <div className="mt-4 space-y-3">
        {data?.items.slice(0, 6).map((item) => (
          <div key={item.key}>
            <div className="mb-1 flex items-center justify-between gap-3 text-xs">
              <span className="truncate text-[var(--color-foreground)]" title={formatLabel(item.key, item.label)}>
                {formatLabel(item.key, item.label)}
              </span>
              <span className="shrink-0 font-mono tabular-nums text-[var(--color-muted-foreground)]">
                {formatTokenCount(item.metrics.total_tokens)}
              </span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-[var(--color-muted)]">
              <div
                className="h-full rounded-full bg-[var(--color-accent)]"
                style={{
                  width: `${Math.max((item.metrics.total_tokens / maximum) * 100, 2)}%`,
                }}
              />
            </div>
          </div>
        ))}
        {(unavailable || !data || data.items.length === 0) && (
          <p className="py-6 text-center text-xs text-[var(--color-muted-foreground)]">
            {unavailable ? unavailableLabel : emptyLabel}
          </p>
        )}
      </div>
    </article>
  );
}

function coverageText(
  value: StatsCoverageCount,
  t: (key: string, variables?: Record<string, string | number>) => string,
) {
  if (value.applicable_rows > 0 && value.known_rows === 0) {
    return t("tokenStats.coverageUnavailable", {
      applicable: value.applicable_rows,
    });
  }
  return t("tokenStats.coverageValue", {
    known: value.known_rows,
    applicable: value.applicable_rows,
  });
}

export function TokenStats() {
  const { t } = useTranslation();
  const [range, setRange] = useState<RangeKey>("24h");
  const [filters, setFilters] = useState<DashboardFilters>(EMPTY_FILTERS);
  const [options, setOptions] = useState<StatsFilters | null>(null);
  const [overview, setOverview] = useState<StatsOverview | null>(null);
  const [breakdowns, setBreakdowns] = useState<Partial<Record<StatsDimension, StatsBreakdown>>>({});
  const [activity, setActivity] = useState<StatsActivity | null>(null);
  const [events, setEvents] = useState<StatsEvent[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [trendMetric, setTrendMetric] = useState<TrendMetric>("tokens");
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [loadMoreError, setLoadMoreError] = useState("");
  const [unavailable, setUnavailable] = useState<string[]>([]);
  const [refreshAt, setRefreshAt] = useState(() => Date.now());
  const queryGenerationRef = useRef(0);
  const paginationControllerRef = useRef<AbortController | null>(null);
  const invalidatePagination = useCallback(() => {
    ++queryGenerationRef.current;
    paginationControllerRef.current?.abort();
    paginationControllerRef.current = null;
    setLoadingMore(false);
  }, []);

  const timezone = useMemo(
    () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    [],
  );
  const query = useMemo<StatsQuery>(
    () => ({
      ...rangeToQuery(range, refreshAt),
      timezone,
      team_id: filters.team || undefined,
      origin: filters.origin || undefined,
      usage_type: filters.usageType || undefined,
      task_type: filters.taskType || undefined,
      model_id: filters.model || undefined,
      status: filters.status || undefined,
    }),
    [filters, range, refreshAt, timezone],
  );
  const refresh = useCallback(() => {
    invalidatePagination();
    setRefreshAt((current) => Math.max(Date.now(), current + 1));
  }, [invalidatePagination]);

  useEffect(() => {
    const controller = new AbortController();
    const generation = ++queryGenerationRef.current;
    let active = true;
    paginationControllerRef.current?.abort();
    paginationControllerRef.current = null;
    setLoadingMore(false);
    async function load() {
      setRefreshing(true);
      setError("");
      setLoadMoreError("");
      try {
        const nextOverview = await getStatsOverview(query, controller.signal);
        if (!active || generation !== queryGenerationRef.current) return;

        const rangeOnly = { from: query.from, to: query.to, timezone: query.timezone };
        const activityQuery = { ...query };
        delete activityQuery.from;
        delete activityQuery.to;
        const results = await Promise.allSettled([
          getStatsFilters(rangeOnly, controller.signal),
          ...DIMENSIONS.map((dimension) =>
            getStatsBreakdown(dimension, query, controller.signal),
          ),
          getStatsActivity(activityQuery, 365, controller.signal),
          getStatsEvents(query, undefined, 25, controller.signal),
        ]);
        if (!active || generation !== queryGenerationRef.current) return;

        const failed: string[] = [];
        const filterResult = results[0];
        if (filterResult.status === "fulfilled") setOptions(filterResult.value);
        else {
          setOptions(null);
          failed.push("filters");
        }

        const nextBreakdowns: Partial<Record<StatsDimension, StatsBreakdown>> = {};
        DIMENSIONS.forEach((dimension, index) => {
          const result = results[index + 1];
          if (result.status === "fulfilled") {
            nextBreakdowns[dimension] = result.value as StatsBreakdown;
          } else failed.push(dimension);
        });
        setBreakdowns(nextBreakdowns);

        const activityResult = results[DIMENSIONS.length + 1];
        if (activityResult.status === "fulfilled") {
          setActivity(activityResult.value as StatsActivity);
        } else {
          setActivity(null);
          failed.push("activity");
        }

        const eventResult = results[DIMENSIONS.length + 2];
        if (eventResult.status === "fulfilled") {
          const eventPage = eventResult.value as Awaited<
            ReturnType<typeof getStatsEvents>
          >;
          setEvents(eventPage.items);
          setNextCursor(eventPage.next_cursor);
        } else {
          setEvents([]);
          setNextCursor(null);
          failed.push("events");
        }
        setOverview(nextOverview);
        setUnavailable(failed);
      } catch (reason) {
        if (
          !active ||
          controller.signal.aborted ||
          generation !== queryGenerationRef.current
        )
          return;
        setError(reason instanceof Error ? reason.message : String(reason));
      } finally {
        if (active && generation === queryGenerationRef.current) {
          setLoading(false);
          setRefreshing(false);
        }
      }
    }
    void load();
    return () => {
      active = false;
      controller.abort();
      paginationControllerRef.current?.abort();
      paginationControllerRef.current = null;
    };
  }, [query]);

  useEffect(() => {
    const timer = window.setInterval(refresh, 60_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const loadMore = async () => {
    if (!nextCursor || loadingMore) return;
    const generation = queryGenerationRef.current;
    const controller = new AbortController();
    paginationControllerRef.current?.abort();
    paginationControllerRef.current = controller;
    setLoadingMore(true);
    setLoadMoreError("");
    try {
      const page = await getStatsEvents(query, nextCursor, 25, controller.signal);
      if (
        controller.signal.aborted ||
        generation !== queryGenerationRef.current ||
        paginationControllerRef.current !== controller
      )
        return;
      setEvents((current) => [...current, ...page.items]);
      setNextCursor(page.next_cursor);
    } catch (reason) {
      if (
        controller.signal.aborted ||
        generation !== queryGenerationRef.current ||
        paginationControllerRef.current !== controller
      )
        return;
      setLoadMoreError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      if (paginationControllerRef.current === controller) {
        paginationControllerRef.current = null;
        setLoadingMore(false);
      }
    }
  };

  const updateFilter = (key: keyof DashboardFilters, value: string) => {
    if (filters[key] === value) return;
    invalidatePagination();
    setFilters((current) => ({ ...current, [key]: value }));
  };

  const updateRange = (value: RangeKey) => {
    if (range === value) return;
    invalidatePagination();
    setRange(value);
  };

  const resetFilters = () => {
    invalidatePagination();
    setFilters(EMPTY_FILTERS);
  };

  if (loading && !overview) {
    return (
      <section className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] py-14 text-center text-[var(--color-muted-foreground)] animate-slide-up">
        <Loader2 className="mx-auto h-6 w-6 animate-spin" />
        <p className="mt-3 text-sm">{t("tokenStats.loading")}</p>
      </section>
    );
  }

  if (error && !overview) {
    return (
      <section className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] py-14 text-center animate-slide-up">
        {error === "db_unavailable" ? (
          <Database className="mx-auto h-6 w-6 text-[var(--color-muted-foreground)]" />
        ) : (
          <AlertCircle className="mx-auto h-6 w-6 text-[var(--color-destructive)]" />
        )}
        <p className="mt-3 text-sm text-[var(--color-muted-foreground)]">
          {error === "db_unavailable" ? t("tokenStats.dbUnavailable") : t("tokenStats.loadError")}
        </p>
        <button
          type="button"
          onClick={refresh}
          className="mt-4 min-h-10 rounded-lg border border-[var(--color-border)] px-4 text-xs font-medium text-[var(--color-foreground)]"
        >
          {t("tokenStats.retry")}
        </button>
      </section>
    );
  }

  if (!overview) return null;
  const summary = overview.summary;
  const coverage = overview.meta.coverage;
  const allLegacy = coverage.total_rows > 0 && coverage.legacy_rows === coverage.total_rows;
  const hasReliability = coverage.status.known_rows > 0;
  const hasLatency = coverage.latency.known_rows > 0;
  const hasCache = coverage.cache_detail.known_rows > 0;
  const hasReasoning = coverage.reasoning_detail.known_rows > 0;
  const hasTaskTypes = coverage.task_type.known_rows > 0;
  const hasOrigins = coverage.origin.known_rows > 0;
  const trendMetrics: TrendMetric[] = [
    "tokens",
    "calls",
    ...(hasReliability ? (["errors"] as TrendMetric[]) : []),
    ...(hasLatency ? (["latency"] as TrendMetric[]) : []),
  ];
  const currentTrend = trendMetrics.includes(trendMetric) ? trendMetric : "tokens";
  const fullTimestampFormatter = new Intl.DateTimeFormat(undefined, {
    timeZone: overview.meta.timezone,
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    timeZoneName: "short",
  });
  const chartData = overview.series.map((point) => ({
    key: point.start,
    label: pointLabel(point.start, overview.meta.bucket_size, overview.meta.timezone),
    timestamp: fullTimestampFormatter.format(new Date(point.start)),
    value: metricValue(point.metrics, currentTrend),
  }));
  const tickInterval =
    chartData.length <= 6 ? 1 : Math.ceil((chartData.length - 1) / 5);
  const maximum = Math.max(...chartData.map((point) => point.value ?? 0), 1);
  const coverageItems: [string, StatsCoverageCount][] = [
    ...(coverage.status.applicable_rows > 0 ? [[t("tokenStats.reliability"), coverage.status] as [string, StatsCoverageCount]] : []),
    ...(coverage.latency.applicable_rows > 0 ? [[t("tokenStats.latency"), coverage.latency] as [string, StatsCoverageCount]] : []),
    ...(coverage.cache_detail.applicable_rows > 0 ? [[t("tokenStats.cache"), coverage.cache_detail] as [string, StatsCoverageCount]] : []),
    ...(coverage.reasoning_detail.applicable_rows > 0 ? [[t("tokenStats.reasoning"), coverage.reasoning_detail] as [string, StatsCoverageCount]] : []),
    ...(coverage.task_type.applicable_rows > 0 ? [[t("tokenStats.taskType"), coverage.task_type] as [string, StatsCoverageCount]] : []),
    ...(coverage.origin.applicable_rows > 0 ? [[t("tokenStats.origin"), coverage.origin] as [string, StatsCoverageCount]] : []),
  ];
  const breakdownCards: { dimension: StatsDimension; title: string }[] = [
    { dimension: "model", title: t("tokenStats.byModel") },
    { dimension: "usage_type", title: t("tokenStats.byUsageType") },
    { dimension: "team", title: t("tokenStats.byTeam") },
    ...(hasTaskTypes ? [{ dimension: "task_type" as const, title: t("tokenStats.byTaskType") }] : []),
    ...(hasOrigins ? [{ dimension: "origin" as const, title: t("tokenStats.byOrigin") }] : []),
    ...(hasReliability ? [{ dimension: "status" as const, title: t("tokenStats.byStatus") }] : []),
  ];
  const hasFilters = Object.values(filters).some(Boolean);
  const statusLabel = (value: string) => {
    const labels: Record<string, string> = {
      success: t("tokenStats.statusSuccess"),
      error: t("tokenStats.statusError"),
      cancelled: t("tokenStats.statusCancelled"),
      timeout: t("tokenStats.statusTimeout"),
    };
    return labels[value] ?? value;
  };
  const statusOptions = options?.statuses.map((option) => ({
    ...option,
    label: statusLabel(option.value),
  }));

  return (
    <section className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] shadow-sm animate-slide-up">
      <header className="flex flex-col gap-4 border-b border-[var(--color-border)] px-4 py-4 sm:px-6 lg:flex-row lg:items-center lg:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <BarChart3 className="h-4 w-4 text-[var(--color-accent)]" />
            <h2 className="text-sm font-semibold text-[var(--color-foreground)]">{t("tokenStats.title")}</h2>
            {coverage.legacy_rows > 0 && (
              <span className="rounded-full border border-[color-mix(in_srgb,var(--color-warning)_35%,transparent)] px-2 py-0.5 text-[10px] font-medium text-[var(--color-warning)]">
                {coverage.legacy_rows.toLocaleString()} {t("tokenStats.legacy")}
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{t("tokenStats.subtitle")}</p>
          <p className="mt-1 font-mono text-[10px] text-[var(--color-muted-foreground)]">
            {t("tokenStats.updated")} {new Date(overview.meta.generated_at).toLocaleTimeString()} · {overview.meta.timezone}
          </p>
        </div>
        <button
          type="button"
          onClick={refresh}
          disabled={refreshing}
          aria-busy={refreshing}
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-lg border border-[var(--color-border)] px-4 text-xs font-medium text-[var(--color-foreground)] disabled:opacity-50"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`} />
          {refreshing ? t("tokenStats.refreshing") : t("tokenStats.refresh")}
        </button>
      </header>

      <div className="space-y-5 p-4 sm:p-6">
        {error && (
          <div role="alert" className="flex items-start gap-2 rounded-lg border border-[var(--color-destructive)] px-3 py-2 text-xs text-[var(--color-destructive)]">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            {t("tokenStats.refreshError")}
          </div>
        )}
        {unavailable.length > 0 && (
          <div role="status" className="flex items-start gap-2 rounded-lg border border-[var(--color-warning)] px-3 py-2 text-xs text-[var(--color-warning)]">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            {t("tokenStats.partialError")}
          </div>
        )}

        <section aria-label={t("tokenStats.filtersTitle")}>
          <div className="flex flex-wrap items-center gap-2">
            {(["24h", "7d", "30d"] as RangeKey[]).map((key) => (
              <button
                key={key}
                type="button"
                aria-pressed={range === key}
                onClick={() => updateRange(key)}
                className={`min-h-10 rounded-lg border px-4 text-xs font-medium ${range === key ? "border-[var(--color-accent)] bg-[color-mix(in_srgb,var(--color-accent)_16%,transparent)] text-[var(--color-accent)]" : "border-[var(--color-border)] text-[var(--color-muted-foreground)]"}`}
              >
                {t(`tokenStats.preset${key}`)}
              </button>
            ))}
            {hasFilters && (
              <button type="button" onClick={resetFilters} className="inline-flex min-h-10 items-center gap-2 rounded-lg px-3 text-xs font-medium text-[var(--color-muted-foreground)]">
                <FilterX className="h-3.5 w-3.5" />
                {t("tokenStats.resetFilters")}
              </button>
            )}
          </div>
          <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            <SelectField label={t("tokenStats.team")} value={filters.team} options={options?.teams} allLabel={t("tokenStats.allTeams")} disabled={unavailable.includes("filters")} onChange={(value) => updateFilter("team", value)} />
            <SelectField label={t("tokenStats.origin")} value={filters.origin} options={options?.origins} allLabel={t("tokenStats.allOrigins")} disabled={unavailable.includes("filters")} onChange={(value) => updateFilter("origin", value)} />
            <SelectField label={t("tokenStats.usageType")} value={filters.usageType} options={options?.usage_types} allLabel={t("tokenStats.allUsageTypes")} disabled={unavailable.includes("filters")} onChange={(value) => updateFilter("usageType", value)} />
            <SelectField label={t("tokenStats.taskType")} value={filters.taskType} options={options?.task_types} allLabel={t("tokenStats.allTaskTypes")} disabled={unavailable.includes("filters")} onChange={(value) => updateFilter("taskType", value)} />
            <SelectField label={t("tokenStats.model")} value={filters.model} options={options?.models} allLabel={t("tokenStats.allModels")} disabled={unavailable.includes("filters")} onChange={(value) => updateFilter("model", value)} />
            <SelectField label={t("tokenStats.status")} value={filters.status} options={statusOptions} allLabel={t("tokenStats.allStatuses")} disabled={unavailable.includes("filters")} onChange={(value) => updateFilter("status", value)} />
          </div>
        </section>

        <section aria-label={t("tokenStats.summaryTitle")} className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <Kpi label={t("tokenStats.summaryTotal")} value={formatTokenCount(summary.total_tokens)} delta={overview.comparison.total_tokens?.change_pct} hint={t("tokenStats.previousPeriod")} icon={<Zap className="h-4 w-4" />} />
          <Kpi label={t("tokenStats.requests")} value={summary.request_count.toLocaleString()} delta={overview.comparison.request_count?.change_pct} hint={t("tokenStats.previousPeriod")} icon={<Activity className="h-4 w-4" />} />
          {hasReliability && summary.success_rate !== null && <Kpi label={t("tokenStats.successRate")} value={`${(summary.success_rate * 100).toFixed(1)}%`} hint={coverageText(coverage.status, t)} icon={<Gauge className="h-4 w-4" />} />}
          {hasLatency && summary.p95_duration_ms !== null && <Kpi label={t("tokenStats.p95Latency")} value={formatDuration(summary.p95_duration_ms)} hint={coverageText(coverage.latency, t)} icon={<Clock3 className="h-4 w-4" />} />}
          {hasCache && summary.cache_hit_rate !== null && <Kpi label={t("tokenStats.summaryCacheHit")} value={`${(summary.cache_hit_rate * 100).toFixed(1)}%`} hint={coverageText(coverage.cache_detail, t)} icon={<Database className="h-4 w-4" />} />}
          {hasReasoning && <Kpi label={t("tokenStats.reasoningTokens")} value={formatTokenCount(summary.reasoning_tokens)} hint={coverageText(coverage.reasoning_detail, t)} icon={<Sparkles className="h-4 w-4" />} />}
        </section>

        {coverage.total_rows === 0 ? (
          <div className="rounded-xl border border-[var(--color-border)] py-10 text-center text-xs text-[var(--color-muted-foreground)]">{t("tokenStats.noData")}</div>
        ) : (
          <section aria-label={t("tokenStats.coverageTitle")} className="rounded-xl border border-[var(--color-border)] p-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <h3 className="text-xs font-semibold text-[var(--color-foreground)]">{t("tokenStats.coverageTitle")}</h3>
                <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                  {allLegacy ? t("tokenStats.legacyOnly") : coverage.legacy_rows > 0 ? t("tokenStats.mixedCoverage", { count: coverage.legacy_rows }) : t("tokenStats.currentCoverage")}
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                {coverageItems.map(([label, value]) => (
                  <span key={label} className="rounded-full border border-[var(--color-border)] px-2 py-1 text-[10px] text-[var(--color-muted-foreground)]">
                    {label}: {coverageText(value, t)}
                  </span>
                ))}
              </div>
            </div>
          </section>
        )}

        <section aria-label={t("tokenStats.trendTitle")} className="rounded-xl border border-[var(--color-border)] p-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h3 className="text-xs font-semibold text-[var(--color-foreground)]">{t("tokenStats.trendTitle")}</h3>
            <div className="flex flex-wrap gap-1" aria-label={t("tokenStats.trendMetric")}>
              {trendMetrics.map((metric) => (
                <button key={metric} type="button" aria-pressed={currentTrend === metric} onClick={() => setTrendMetric(metric)} className={`min-h-10 rounded-lg px-3 text-xs font-medium ${currentTrend === metric ? "bg-[color-mix(in_srgb,var(--color-accent)_16%,transparent)] text-[var(--color-accent)]" : "text-[var(--color-muted-foreground)]"}`}>
                  {t(`tokenStats.trend${metric}`)}
                </button>
              ))}
            </div>
          </div>
          {chartData.length === 0 ? (
            <p className="py-10 text-center text-xs text-[var(--color-muted-foreground)]">{t("tokenStats.noData")}</p>
          ) : (
            <div className="mt-4 flex h-44 items-end gap-2 overflow-x-auto pb-1">
              {chartData.map((point, index) => {
                const height = point.value === null ? 0 : (point.value / maximum) * 100;
                const formattedValue =
                  point.value === null
                    ? t("tokenStats.unavailable")
                    : currentTrend === "latency"
                      ? formatDuration(point.value)
                      : formatTokenCount(point.value);
                const showTick =
                  index === 0 ||
                  index === chartData.length - 1 ||
                  index % tickInterval === 0;
                return (
                  <div
                    key={point.key}
                    role="img"
                    aria-label={`${point.timestamp}: ${formattedValue}`}
                    title={`${point.timestamp}: ${formattedValue}`}
                    className="flex h-full min-w-7 flex-1 flex-col items-center justify-end gap-1"
                  >
                    <span className="text-[9px] font-mono text-[var(--color-muted-foreground)]">
                      {formattedValue}
                    </span>
                    <div className="flex h-full w-full items-end overflow-hidden rounded-sm bg-[var(--color-muted)]">
                      <div className="w-full rounded-t-sm bg-[var(--color-accent)]" style={{ height: `${height}%`, minHeight: height > 0 ? 2 : 0 }} />
                    </div>
                    <span className="whitespace-nowrap text-[9px] text-[var(--color-muted-foreground)]">
                      {showTick ? point.label : "\u00a0"}
                    </span>
                  </div>
                );
              })}
            </div>
          )}
        </section>

        {overview.insights.length > 0 && (
          <section aria-label={t("tokenStats.insightsTitle")}>
            <h3 className="mb-3 text-xs font-semibold text-[var(--color-foreground)]">{t("tokenStats.insightsTitle")}</h3>
            <div className="grid gap-3 sm:grid-cols-2">
              {overview.insights.map((insight) => {
                const knownTitle = insight.id === "reliability-errors" ? t("tokenStats.errorsDetectedTitle") : insight.id === "token-change" ? t("tokenStats.tokenChangeTitle") : insight.title;
                const knownDetail = insight.id === "reliability-errors" ? t("tokenStats.errorsDetectedDetail", { count: summary.error_count + summary.timeout_count }) : insight.id === "token-change" && insight.change_pct !== null ? t("tokenStats.tokenChangeDetail", { change: insight.change_pct.toFixed(1) }) : insight.detail;
                return (
                  <article key={insight.id} className={`rounded-xl border p-4 ${insight.severity === "warning" ? "border-[var(--color-warning)]" : "border-[var(--color-border)]"}`}>
                    <p className="text-xs font-semibold text-[var(--color-foreground)]">{knownTitle}</p>
                    <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">{knownDetail}</p>
                  </article>
                );
              })}
            </div>
          </section>
        )}

        <section aria-label={t("tokenStats.breakdownsTitle")}>
          <h3 className="mb-3 text-xs font-semibold text-[var(--color-foreground)]">{t("tokenStats.breakdownsTitle")}</h3>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {breakdownCards.map(({ dimension, title }) => (
              <BreakdownCard key={dimension} title={title} data={breakdowns[dimension]} unavailable={unavailable.includes(dimension)} emptyLabel={t("tokenStats.noDataForSelection")} unavailableLabel={t("tokenStats.sectionUnavailable")} formatLabel={dimension === "status" ? (key) => statusLabel(key) : undefined} />
            ))}
          </div>
        </section>

        <section aria-label={t("tokenStats.activityTitle")} className="rounded-xl border border-[var(--color-border)] p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-xs font-semibold text-[var(--color-foreground)]">{t("tokenStats.activityTitle")}</h3>
            {activity && <span className="text-[10px] text-[var(--color-muted-foreground)]">{t("tokenStats.activitySummary", { days: activity.active_days, tokens: formatTokenCount(activity.total_tokens) })}</span>}
          </div>
          {unavailable.includes("activity") ? (
            <p className="py-8 text-center text-xs text-[var(--color-muted-foreground)]">{t("tokenStats.sectionUnavailable")}</p>
          ) : activity && activity.points.length > 0 ? (
            <div className="mt-4 overflow-x-auto pb-2">
              <div className="grid w-max grid-flow-col grid-rows-7 gap-1" role="img" aria-label={t("tokenStats.activityAria")}>
                {activity.points.map((point) => (
                  <span key={point.date} title={t("tokenStats.activityDay", { date: point.date, requests: point.request_count, tokens: formatTokenCount(point.total_tokens) })} className="h-3 w-3 rounded-[3px]" style={{ backgroundColor: point.level === 0 ? "var(--color-muted)" : `color-mix(in srgb, var(--color-accent) ${25 + point.level * 18}%, transparent)` }} />
                ))}
              </div>
            </div>
          ) : (
            <p className="py-8 text-center text-xs text-[var(--color-muted-foreground)]">{t("tokenStats.noActivity")}</p>
          )}
        </section>

        <section aria-label={t("tokenStats.eventsTitle")} className="overflow-hidden rounded-xl border border-[var(--color-border)]">
          <div className="flex items-center justify-between border-b border-[var(--color-border)] px-4 py-3">
            <h3 className="text-xs font-semibold text-[var(--color-foreground)]">{t("tokenStats.eventsTitle")}</h3>
            <span className="text-[10px] text-[var(--color-muted-foreground)]">{t("tokenStats.eventsCount", { count: events.length })}</span>
          </div>
          {unavailable.includes("events") ? (
            <p className="py-10 text-center text-xs text-[var(--color-muted-foreground)]">{t("tokenStats.sectionUnavailable")}</p>
          ) : events.length === 0 ? (
            <p className="py-10 text-center text-xs text-[var(--color-muted-foreground)]">{t("tokenStats.noEvents")}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[720px] text-left text-xs">
                <thead className="border-b border-[var(--color-border)] text-[10px] uppercase tracking-wider text-[var(--color-muted-foreground)]">
                  <tr>
                    <th className="px-4 py-2 font-medium">{t("tokenStats.time")}</th>
                    <th className="px-4 py-2 font-medium">{t("tokenStats.model")}</th>
                    <th className="px-4 py-2 font-medium">{t("tokenStats.context")}</th>
                    <th className="px-4 py-2 font-medium">{t("tokenStats.status")}</th>
                    <th className="px-4 py-2 text-right font-medium">{t("tokenStats.tokens")}</th>
                    <th className="px-4 py-2 text-right font-medium">{t("tokenStats.duration")}</th>
                  </tr>
                </thead>
                <tbody>
                  {events.map((event) => {
                    const context = [event.origin, event.usage_type, event.task_type].filter(Boolean);
                    const hasModel = event.model_id && event.model_id !== "unknown";
                    const model = hasModel ? `${event.provider_id ? `${event.provider_id}/` : ""}${event.model_id}` : event.legacy ? t("tokenStats.legacy") : t("tokenStats.unavailable");
                    return (
                      <tr key={event.call_id} className="border-t border-[var(--color-border)]">
                        <td className="whitespace-nowrap px-4 py-3 font-mono text-[var(--color-muted-foreground)]">{new Date(event.finished_at).toLocaleString()}</td>
                        <td className="max-w-52 truncate px-4 py-3 text-[var(--color-foreground)]" title={model}>{model}</td>
                        <td className="px-4 py-3 text-[var(--color-muted-foreground)]">{context.length > 0 ? context.join(" · ") : event.legacy ? t("tokenStats.legacy") : t("tokenStats.unavailable")}</td>
                        <td className="px-4 py-3">
                          <span className={`rounded-full border border-[var(--color-border)] px-2 py-0.5 text-[10px] font-medium ${event.status === "success" ? "text-[var(--color-success)]" : event.status === null ? "text-[var(--color-muted-foreground)]" : "text-[var(--color-destructive)]"}`}>
                            {event.legacy ? t("tokenStats.legacy") : event.status ? statusLabel(event.status) : t("tokenStats.unavailable")}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-right font-mono tabular-nums">{event.total_tokens.toLocaleString()}</td>
                        <td className="px-4 py-3 text-right font-mono tabular-nums text-[var(--color-muted-foreground)]">{event.duration_ms === null ? event.legacy ? t("tokenStats.legacy") : t("tokenStats.unavailable") : formatDuration(event.duration_ms)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
          {(nextCursor || loadMoreError) && (
            <div className="border-t border-[var(--color-border)] px-4 py-3 text-center">
              {loadMoreError && <p role="alert" className="mb-2 text-xs text-[var(--color-destructive)]">{t("tokenStats.paginationError")}</p>}
              {nextCursor && (
                <button type="button" onClick={() => void loadMore()} disabled={loadingMore} aria-busy={loadingMore} className="min-h-10 rounded-lg border border-[var(--color-border)] px-4 text-xs font-medium text-[var(--color-foreground)] disabled:opacity-50">
                  {loadingMore ? t("tokenStats.loadingMore") : loadMoreError ? t("tokenStats.retryLoadMore") : t("tokenStats.loadMore")}
                </button>
              )}
            </div>
          )}
        </section>
      </div>
    </section>
  );
}
