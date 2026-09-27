import type { ReactNode } from "react";
import { useI18n } from "../../lib/i18n";
import { formatBytes, formatDuration, type RuntimeResponse } from "../../lib/metrics";
import type { GameServer } from "../../lib/types";
import { Card } from "../ui/Card";

const REASON_KEY = {
  OOMKilled: "metrics.reasonOOMKilled",
  Error: "metrics.reasonError",
  Completed: "metrics.reasonCompleted",
} as const;

function StatCard({ title, value, detail, children }: { title: string; value: string; detail?: string; children?: ReactNode }) {
  return (
    <Card className="flex flex-col gap-2 p-4">
      <span className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{title}</span>
      <span className="font-display text-xl font-bold text-text-primary">{value}</span>
      {children}
      {detail && <span className="font-prose text-xs text-text-secondary">{detail}</span>}
    </Card>
  );
}

export function RuntimeCards({ runtime, now, server }: { runtime: RuntimeResponse; now: number; server: GameServer }) {
  const { t, formatDateTime } = useI18n();
  const { startedAt, terminated, disk } = runtime;

  const term = terminated;
  const reasonLabel = term?.reason
    ? term.reason in REASON_KEY
      ? t(REASON_KEY[term.reason as keyof typeof REASON_KEY])
      : term.reason
    : undefined;
  const termValue = term
    ? [term.container === "install" ? t("metrics.runtimeInstall") : undefined, reasonLabel].filter(Boolean).join(" · ") ||
      t("metrics.reasonError")
    : t("metrics.runtimeNoIssue");
  const termDetail = term
    ? [
        t("metrics.runtimeExit", { code: term.exitCode }),
        term.finishedAt ? t("metrics.runtimeEndedAgo", { time: formatDuration(now - new Date(term.finishedAt).getTime()) }) : undefined,
      ]
        .filter(Boolean)
        .join(" · ")
    : undefined;
  const hint = term?.container === "install" ? t("metrics.installHint") : term?.reason === "OOMKilled" ? t("metrics.oomHint") : undefined;

  const players = server.players;
  const running = server.status?.phase === "Running";
  const names = players?.names ?? [];
  const shown = names.slice(0, 12);
  const playersDetail = players
    ? players.online === 0
      ? t("metrics.playersNone")
      : undefined
    : running
      ? t("metrics.playersNoAnswer")
      : undefined;

  const percent = disk?.totalBytes ? Math.min(100, Math.round((disk.usedBytes / disk.totalBytes) * 100)) : undefined;

  return (
    <div className="flex flex-col gap-3">
      <div className={server.queryEnabled ? "grid gap-4 sm:grid-cols-2 xl:grid-cols-4" : "grid gap-4 sm:grid-cols-3"}>
        <StatCard
          title={t("metrics.runtimeUptime")}
          value={startedAt ? formatDuration(now - new Date(startedAt).getTime()) : "—"}
          detail={startedAt ? t("metrics.runtimeSince", { time: formatDateTime(startedAt) }) : undefined}
        />
        <StatCard title={t("metrics.runtimeLastRun")} value={termValue} detail={termDetail} />
        <StatCard
          title={t("metrics.disk")}
          value={disk ? formatBytes(disk.usedBytes) : "—"}
          detail={
            disk
              ? disk.totalBytes
                ? `${t("metrics.diskUsed", { used: formatBytes(disk.usedBytes), total: formatBytes(disk.totalBytes) })}${percent !== undefined ? ` · ${percent}%` : ""}`
                : t("metrics.diskUsedOnly", { used: formatBytes(disk.usedBytes) })
              : startedAt
                ? t("metrics.diskUnavailable")
                : undefined
          }
        >
          {percent !== undefined && (
            <div role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100} className="h-1.5 w-full bg-border">
              <div className="h-full bg-primary" style={{ width: `${percent}%` }} />
            </div>
          )}
        </StatCard>
        {server.queryEnabled && (
          <StatCard title={t("metrics.players")} value={players ? `${players.online}/${players.max}` : "—"} detail={playersDetail}>
            {shown.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {shown.map((n) => (
                  <span key={n} className="border border-border px-1.5 py-0.5 font-sans text-xs text-text-secondary">
                    {n}
                  </span>
                ))}
                {names.length > shown.length && (
                  <span className="px-1.5 py-0.5 font-sans text-xs text-text-tertiary">
                    {t("metrics.playersMore", { count: names.length - shown.length })}
                  </span>
                )}
              </div>
            )}
          </StatCard>
        )}
      </div>
      {hint && <div className="border border-border-strong bg-surface px-3 py-2 font-prose text-sm text-text-secondary">{hint}</div>}
    </div>
  );
}
