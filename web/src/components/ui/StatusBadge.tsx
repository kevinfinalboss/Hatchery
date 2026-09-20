import type { GameServerPhase } from "../../lib/types";

const phaseConfig: Record<string, { label: string; color: string; dot: string }> = {
  Running: { label: "Running", color: "text-status-running", dot: "bg-status-running" },
  Installing: { label: "Installing", color: "text-status-installing", dot: "bg-status-installing" },
  Pending: { label: "Pending", color: "text-status-installing", dot: "bg-status-installing" },
  Stopping: { label: "Stopping", color: "text-text-secondary", dot: "bg-status-stopped" },
  Stopped: { label: "Stopped", color: "text-text-secondary", dot: "bg-status-stopped" },
  Failed: { label: "Failed", color: "text-status-failed", dot: "bg-status-failed" },
};

const bgFor: Record<string, string> = {
  Running: "bg-status-running/15",
  Installing: "bg-status-installing/15",
  Pending: "bg-status-installing/15",
  Stopping: "bg-status-stopped/15",
  Stopped: "bg-status-stopped/15",
  Failed: "bg-status-failed/15",
};

export function StatusBadge({ phase }: { phase: GameServerPhase | string }) {
  const config = phaseConfig[phase] ?? { label: phase || "—", color: "text-text-tertiary", dot: "bg-text-tertiary" };
  const bg = bgFor[phase] ?? "bg-surface-hover";

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 font-sans text-xs font-semibold ${config.color} ${bg}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${config.dot}`} />
      {config.label}
    </span>
  );
}
