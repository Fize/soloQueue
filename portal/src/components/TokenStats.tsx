import { useEffect, useState } from "react";
import {
  AlertCircle,
  BarChart3,
  ChevronDown,
  ChevronRight,
  Database,
  Loader2,
} from "lucide-react";

import { formatTokenCount } from "../App";
import { useTranslation } from "../i18n";

type PresetKey = "24h" | "today" | "3d" | "7d";

interface Metrics {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cache_hit_tokens: number;
  request_count: number;
  success_rate: number | null;
  cache_hit_rate: number | null;
  p95_duration_ms: number | null;
}

interface Overview {
  meta: {
    generated_at: string;
    timezone: string;
    bucket_size: "hour" | "day" | "week" | "none";
    coverage: {
      total_rows: number;
      legacy_rows: number;
      cache_detail: { known_rows: number; applicable_rows: number };
    };
  };
  summary: Metrics;
  series: { start: string; metrics: Metrics }[];
}

interface Breakdown {
  items: { key: string; label: string; metrics: Metrics }[];
}

interface Envelope<T> {
  data: T | null;
  error: string | null;
}

const PRESETS: { key: PresetKey; labelKey: string }[] = [
  { key: "24h", labelKey: "tokenStats.preset24h" },
  { key: "today", labelKey: "tokenStats.presetToday" },
  { key: "3d", labelKey: "tokenStats.preset3d" },
  { key: "7d", labelKey: "tokenStats.preset7d" },
];

function getRange(preset: PresetKey) {
  const now = new Date();
  let from: Date;
  if (preset === "today")
    from = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  else if (preset === "3d") from = new Date(now.getTime() - 3 * 24 * 3_600_000);
  else if (preset === "7d") from = new Date(now.getTime() - 7 * 24 * 3_600_000);
  else from = new Date(now.getTime() - 24 * 3_600_000);
  return { from: from.toISOString(), to: now.toISOString() };
}

async function fetchEnvelope<T>(path: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal });
  if (!response.ok) {
    if (response.status === 503) throw new Error("db_unavailable");
    throw new Error(`HTTP ${response.status}`);
  }
  const envelope = (await response.json()) as Envelope<T>;
  if (envelope.error || !envelope.data)
    throw new Error(envelope.error || "Invalid statistics response");
  return envelope.data;
}

function labelForPoint(value: string, timezone: string, hourly: boolean) {
  return new Intl.DateTimeFormat(undefined, {
    timeZone: timezone,
    month: "numeric",
    day: "numeric",
    ...(hourly ? { hour: "2-digit" as const } : {}),
  }).format(new Date(value));
}

export function TokenStats() {
  const { t } = useTranslation();
  const [overview, setOverview] = useState<Overview | null>(null);
  const [models, setModels] = useState<Breakdown["items"]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState(true);
  const [preset, setPreset] = useState<PresetKey>("24h");

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    const load = async () => {
      setLoading(true);
      const timezone =
        Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
      const params = new URLSearchParams({ ...getRange(preset), timezone });
      try {
        const next = await fetchEnvelope<Overview>(
          `/api/stats/overview?${params}`,
          controller.signal,
        );
        if (!active) return;
        setOverview(next);
        setError(null);
        const modelParams = new URLSearchParams(params);
        modelParams.set("dimension", "model");
        try {
          const breakdown = await fetchEnvelope<Breakdown>(
            `/api/stats/breakdowns?${modelParams}`,
            controller.signal,
          );
          if (active) setModels(breakdown.items);
        } catch {
          if (active) setModels([]);
        }
      } catch (reason) {
        if (active && !controller.signal.aborted) {
          setError(reason instanceof Error ? reason.message : String(reason));
        }
      } finally {
        if (active) setLoading(false);
      }
    };
    void load();
    const timer = window.setInterval(load, 60_000);
    return () => {
      active = false;
      controller.abort();
      window.clearInterval(timer);
    };
  }, [preset]);

  if (loading && !overview) {
    return (
      <section
        className="rounded-xl overflow-hidden animate-slide-up"
        style={{ backgroundColor: "var(--color-card)" }}
      >
        <div className="flex flex-col items-center justify-center py-12 gap-3">
          <Loader2
            className="h-6 w-6 animate-spin"
            style={{ color: "var(--color-muted-foreground)" }}
          />
          <span
            className="text-sm"
            style={{ color: "var(--color-muted-foreground)" }}
          >
            {t("tokenStats.loading")}
          </span>
        </div>
      </section>
    );
  }

  if (error && !overview) {
    return (
      <section
        className="rounded-xl overflow-hidden animate-slide-up"
        style={{ backgroundColor: "var(--color-card)" }}
      >
        <div className="flex flex-col items-center justify-center py-12 gap-3">
          {error === "db_unavailable" ? (
            <Database
              className="h-6 w-6"
              style={{ color: "var(--color-muted-foreground)" }}
            />
          ) : (
            <AlertCircle
              className="h-6 w-6"
              style={{ color: "var(--color-destructive)" }}
            />
          )}
          <span
            className="text-sm"
            style={{ color: "var(--color-muted-foreground)" }}
          >
            {error === "db_unavailable" ? t("tokenStats.dbUnavailable") : error}
          </span>
        </div>
      </section>
    );
  }

  if (!overview) return null;
  const summary = overview.summary;
  const maximum = Math.max(
    ...overview.series.map((point) => point.metrics.total_tokens),
    1,
  );
  const hourly = overview.meta.bucket_size === "hour";
  const cacheCovered = overview.meta.coverage.cache_detail.known_rows > 0;
  const summaryItems = [
    [t("tokenStats.summaryTotal"), formatTokenCount(summary.total_tokens)],
    [t("tokenStats.requests"), summary.request_count.toLocaleString()],
    ...(cacheCovered && summary.cache_hit_rate !== null
      ? [
          [
            t("tokenStats.summaryCacheHit"),
            `${(summary.cache_hit_rate * 100).toFixed(1)}%`,
          ],
        ]
      : []),
  ];

  return (
    <section
      className="rounded-xl overflow-hidden animate-slide-up shadow-sm"
      style={{ backgroundColor: "var(--color-card)" }}
    >
      <div
        className="px-4 sm:px-6 py-3 border-b flex items-center justify-between flex-wrap gap-2"
        style={{ borderColor: "var(--color-border)" }}
      >
        <button
          type="button"
          className="flex items-center gap-2"
          onClick={() => setCollapsed((value) => !value)}
        >
          <BarChart3
            className="h-4 w-4"
            style={{ color: "var(--color-accent)" }}
          />
          <span
            className="text-sm font-semibold"
            style={{ color: "var(--color-foreground)" }}
          >
            {t("tokenStats.title")}
          </span>
          {overview.meta.coverage.legacy_rows > 0 && (
            <span
              className="rounded-full px-2 py-0.5 text-[10px]"
              style={{
                backgroundColor: "var(--color-surface-secondary)",
                color: "var(--color-muted-foreground)",
              }}
            >
              {overview.meta.coverage.legacy_rows} legacy
            </span>
          )}
          {collapsed ? (
            <ChevronRight className="h-4 w-4" />
          ) : (
            <ChevronDown className="h-4 w-4" />
          )}
        </button>
        <div className="flex items-center gap-1.5 flex-wrap">
          {PRESETS.map((option) => (
            <button
              key={option.key}
              type="button"
              onClick={() => setPreset(option.key)}
              className="px-2.5 py-1 rounded-full text-xs font-medium"
              style={{
                backgroundColor:
                  preset === option.key
                    ? "color-mix(in srgb, var(--color-accent) 15%, transparent)"
                    : "var(--color-surface-secondary)",
                color:
                  preset === option.key
                    ? "var(--color-accent)"
                    : "var(--color-muted-foreground)",
                border: `1px solid ${preset === option.key ? "color-mix(in srgb, var(--color-accent) 40%, transparent)" : "var(--color-border)"}`,
              }}
            >
              {t(option.labelKey)}
            </button>
          ))}
        </div>
      </div>

      <div
        className="px-4 sm:px-6 py-4 grid grid-cols-2 sm:grid-cols-3 gap-3 border-b"
        style={{ borderColor: "var(--color-border)" }}
      >
        {summaryItems.map(([label, value]) => (
          <div key={label} className="flex flex-col items-center gap-1">
            <span
              className="text-xs font-medium"
              style={{ color: "var(--color-muted-foreground)" }}
            >
              {label}
            </span>
            <span
              className="text-lg font-bold tabular-nums"
              style={{ color: "var(--color-foreground)" }}
            >
              {value}
            </span>
          </div>
        ))}
      </div>

      {overview.meta.coverage.legacy_rows ===
        overview.meta.coverage.total_rows &&
        overview.meta.coverage.total_rows > 0 && (
          <div
            className="px-4 sm:px-6 py-2 text-xs border-b"
            style={{
              borderColor: "var(--color-border)",
              color: "var(--color-muted-foreground)",
              backgroundColor: "var(--color-surface-secondary)",
            }}
          >
            {t("tokenStats.legacyOnly")}
          </div>
        )}

      {!collapsed && (
        <div className="grid lg:grid-cols-[2fr_1fr]">
          <div
            className="px-4 sm:px-6 py-5 border-b lg:border-b-0 lg:border-r"
            style={{ borderColor: "var(--color-border)" }}
          >
            <div className="flex items-center justify-between mb-4">
              <h3
                className="text-xs font-semibold"
                style={{ color: "var(--color-muted-foreground)" }}
              >
                {hourly
                  ? t("tokenStats.chartTitleHourly")
                  : t("tokenStats.chartTitleDaily")}
              </h3>
              <span
                className="text-[10px] font-mono"
                style={{ color: "var(--color-muted-foreground)" }}
              >
                {overview.meta.timezone} · {t("tokenStats.updated")}{" "}
                {new Date(overview.meta.generated_at).toLocaleTimeString()}
              </span>
            </div>
            <div className="flex items-end gap-2 h-32 overflow-x-auto">
              {overview.series.map((point) => {
                const height = (point.metrics.total_tokens / maximum) * 100;
                return (
                  <div
                    key={point.start}
                    className="flex-1 min-w-[22px] flex flex-col items-center justify-end gap-1 h-full"
                  >
                    <span
                      className="text-[9px] font-mono"
                      style={{ color: "var(--color-muted-foreground)" }}
                    >
                      {formatTokenCount(point.metrics.total_tokens)}
                    </span>
                    <div
                      className="w-full flex items-end rounded-sm overflow-hidden"
                      style={{
                        height: "100%",
                        backgroundColor: "var(--color-surface-secondary)",
                      }}
                    >
                      <div
                        className="w-full rounded-t-sm"
                        style={{
                          height: `${height}%`,
                          minHeight: height > 0 ? 2 : 0,
                          backgroundColor: "var(--color-accent)",
                          opacity: 0.85,
                        }}
                      />
                    </div>
                    <span
                      className="text-[9px] whitespace-nowrap"
                      style={{ color: "var(--color-muted-foreground)" }}
                    >
                      {labelForPoint(
                        point.start,
                        overview.meta.timezone,
                        hourly,
                      )}
                    </span>
                  </div>
                );
              })}
            </div>
          </div>
          <div className="px-4 sm:px-6 py-5">
            <h3
              className="text-xs font-semibold mb-3"
              style={{ color: "var(--color-muted-foreground)" }}
            >
              {t("tokenStats.modelTitle")}
            </h3>
            <div className="space-y-3">
              {models.slice(0, 5).map((model) => (
                <div
                  key={model.key}
                  className="flex items-center justify-between gap-3 text-xs"
                >
                  <span
                    className="truncate font-mono"
                    title={model.label}
                    style={{ color: "var(--color-foreground)" }}
                  >
                    {model.label}
                  </span>
                  <span
                    className="shrink-0 font-mono tabular-nums"
                    style={{ color: "var(--color-muted-foreground)" }}
                  >
                    {formatTokenCount(model.metrics.total_tokens)}
                  </span>
                </div>
              ))}
              {models.length === 0 && (
                <p
                  className="py-6 text-center text-xs"
                  style={{ color: "var(--color-muted-foreground)" }}
                >
                  {t("tokenStats.noData")}
                </p>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
