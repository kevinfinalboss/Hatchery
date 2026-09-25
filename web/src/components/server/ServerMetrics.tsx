import { type ReactNode, useState } from "react";
import clsx from "clsx";
import { useI18n } from "../../lib/i18n";
import {
  METRICS_RANGES,
  type MetricsRange,
  formatBytes,
  formatCpu,
  memoryUnit,
  niceCeil,
  parseCpuMillicores,
  parseMemoryBytes,
} from "../../lib/metrics";
import type { GameServer } from "../../lib/types";
import { usePersistedChoice } from "../../lib/usePersistedChoice";
import { useServerMetrics } from "../../lib/useServerMetrics";
import { useServerRuntime } from "../../lib/useServerRuntime";
import { RuntimeCards } from "./RuntimeCards";
import { Card } from "../ui/Card";
import { LineChart } from "../ui/LineChart";

const RANGE_LABEL = {
  "15m": "metrics.range15m",
  "1h": "metrics.range1h",
  "6h": "metrics.range6h",
} as const;

function MetricCard({
  title,
  color,
  current,
  percent,
  children,
}: {
  title: string;
  color: string;
  current?: string;
  percent?: string;
  children: ReactNode;
}) {
  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-baseline justify-between gap-3">
        <span className="flex items-center gap-2 font-sans text-xs uppercase tracking-wide text-text-tertiary">
          <span aria-hidden className="inline-block h-0.5 w-3" style={{ background: color }} />
          {title}
        </span>
        <span className="flex items-baseline gap-2">
          {current && <span className="font-display text-xl font-bold text-text-primary">{current}</span>}
          {percent && <span className="font-sans text-xs text-text-secondary">{percent}</span>}
        </span>
      </div>
      {children}
    </Card>
  );
}

export function ServerMetrics({
  org,
  name,
  server,
  running,
}: {
  org: string;
  name: string;
  server: GameServer;
  running: boolean;
}) {
  const { t, locale, formatDateTime } = useI18n();
  const [range, setRange] = useState<MetricsRange>("1h");
  const [samplePref, setSamplePref] = usePersistedChoice<"on" | "off">("hatchery_mock_metrics", ["on", "off"], "off");
  // Dados de exemplo só existem em `npm run dev`; o build de produção nunca os liga.
  const mock = import.meta.env.DEV && samplePref === "on";

  const limits = server.spec.resources?.limits;
  const cpuLimit = parseCpuMillicores(limits?.cpu);
  const memLimit = parseMemoryBytes(limits?.memory);

  const metrics = useServerMetrics({
    org,
    name,
    range,
    enabled: running,
    mock,
    limits: { cpu: cpuLimit, memory: memLimit },
  });

  const runtime = useServerRuntime(org, name, server.status?.phase);
  const runtimeCards = runtime.data ? <RuntimeCards runtime={runtime.data} now={runtime.dataUpdatedAt} /> : null;

  if (!running) {
    return (
      <div className="flex flex-col gap-4">
        {runtimeCards}
        <div className="font-prose text-sm text-text-secondary">{t("metrics.stopped")}</div>
      </div>
    );
  }

  const raw = metrics.status === "ready" ? metrics.data.points : [];
  const cpuPoints = raw.map((p) => ({ t: p.t, v: p.cpuMillicores }));
  const memPoints = raw.map((p) => ({ t: p.t, v: p.memoryBytes }));
  const refreshing = metrics.status === "ready" && metrics.refreshing;

  const overlay =
    metrics.status === "loading"
      ? t("metrics.loading")
      : metrics.status === "unavailable"
        ? t("metrics.unavailable")
        : metrics.status === "error"
          ? `${t("metrics.error")}: ${metrics.message}`
          : raw.length === 0
            ? t("metrics.noData")
            : undefined;

  const cpuData = Math.max(0, ...cpuPoints.map((p) => p.v));
  const cpuBase = Math.max(cpuData * 1.1, cpuLimit ?? 0) || 1000;
  const cpuYMax = niceCeil(cpuBase / 1000) * 1000;

  const memData = Math.max(0, ...memPoints.map((p) => p.v));
  const memBase = Math.max(memData * 1.1, memLimit ?? 0) || 2 ** 30;
  const memScale = memoryUnit(memBase);
  const memYMax = niceCeil(memBase / memScale) * memScale;

  const cpuNow = cpuPoints[cpuPoints.length - 1]?.v;
  const memNow = memPoints[memPoints.length - 1]?.v;
  const percentOf = (now: number | undefined, limit: number | undefined) =>
    now !== undefined && limit ? t("metrics.ofLimit", { percent: Math.round((now / limit) * 100) }) : undefined;

  const formatTime = (ts: number) => new Date(ts).toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });

  return (
    <div className="flex flex-col gap-4">
      {runtimeCards}
      <div className="flex flex-wrap items-center gap-3">
        <div role="group" aria-label={t("metrics.rangeLabel")} className="flex divide-x divide-border border border-border">
          {METRICS_RANGES.map((r) => (
            <button
              key={r}
              type="button"
              aria-pressed={range === r}
              onClick={() => setRange(r)}
              className={clsx(
                "px-3 py-1.5 font-sans text-xs",
                range === r ? "bg-surface-hover text-primary-text" : "text-text-tertiary hover:text-text-primary",
              )}
            >
              {t(RANGE_LABEL[r])}
            </button>
          ))}
        </div>
        {import.meta.env.DEV && (
          <button
            type="button"
            onClick={() => setSamplePref(mock ? "off" : "on")}
            className="ml-auto font-sans text-xs text-text-tertiary hover:text-text-primary hover:underline"
          >
            {mock ? t("metrics.sampleDisable") : t("metrics.sampleEnable")}
          </button>
        )}
      </div>

      {mock && (
        <div className="border border-border-strong bg-surface px-3 py-2 font-prose text-sm text-text-secondary">
          {t("metrics.sampleBanner")}
        </div>
      )}

      <div className="grid gap-4 xl:grid-cols-2">
        <MetricCard
          title={t("metrics.cpu")}
          color="var(--c-chart-cpu)"
          current={cpuNow !== undefined ? formatCpu(cpuNow) : undefined}
          percent={percentOf(cpuNow, cpuLimit)}
        >
          <LineChart
            points={cpuPoints}
            color="var(--c-chart-cpu)"
            yMax={cpuYMax}
            formatValue={formatCpu}
            formatTick={(v) => formatCpu(v, false)}
            formatTime={formatTime}
            limit={cpuLimit}
            limitLabel={cpuLimit ? t("metrics.limit", { value: formatCpu(cpuLimit) }) : undefined}
            label={t("metrics.chartCpu")}
            dim={refreshing}
            overlay={overlay}
          />
        </MetricCard>

        <MetricCard
          title={t("metrics.memory")}
          color="var(--c-chart-mem)"
          current={memNow !== undefined ? formatBytes(memNow) : undefined}
          percent={percentOf(memNow, memLimit)}
        >
          <LineChart
            points={memPoints}
            color="var(--c-chart-mem)"
            yMax={memYMax}
            formatValue={formatBytes}
            formatTick={formatBytes}
            formatTime={formatTime}
            limit={memLimit}
            limitLabel={memLimit ? t("metrics.limit", { value: formatBytes(memLimit) }) : undefined}
            label={t("metrics.chartMemory")}
            dim={refreshing}
            overlay={overlay}
          />
        </MetricCard>
      </div>

      {raw.length > 0 && (
        <details className="border border-border bg-surface">
          <summary className="cursor-pointer px-4 py-2 font-sans text-xs text-text-secondary hover:text-text-primary">
            {t("metrics.table")}
          </summary>
          <div className="max-h-64 overflow-y-auto">
            <table className="w-full text-left font-sans text-xs">
              <thead>
                <tr className="uppercase tracking-wide text-text-tertiary">
                  <th className="px-4 py-2 font-medium">{t("metrics.time")}</th>
                  <th className="px-4 py-2 font-medium">{t("metrics.cpu")}</th>
                  <th className="px-4 py-2 font-medium">{t("metrics.memory")}</th>
                </tr>
              </thead>
              <tbody>
                {[...raw].reverse().map((p) => (
                  <tr key={p.t} className="border-t border-border text-text-secondary">
                    <td className="px-4 py-1.5">{formatDateTime(p.t)}</td>
                    <td className="px-4 py-1.5 tabular-nums">{formatCpu(p.cpuMillicores)}</td>
                    <td className="px-4 py-1.5 tabular-nums">{formatBytes(p.memoryBytes)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
    </div>
  );
}
