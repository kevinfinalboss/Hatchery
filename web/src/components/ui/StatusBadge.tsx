import { useT, type TKey } from "../../lib/i18n";
import type { GameServerPhase } from "../../lib/types";

const phaseConfig: Record<string, { label: TKey; glyph: string; color: string }> = {
  Running: { label: "status.running", glyph: "●", color: "text-status-running" },
  Installing: { label: "status.installing", glyph: "◐", color: "text-status-installing" },
  Starting: { label: "status.starting", glyph: "◐", color: "text-status-installing" },
  Pending: { label: "status.pending", glyph: "◐", color: "text-status-installing" },
  Stopping: { label: "status.stopping", glyph: "◐", color: "text-text-secondary" },
  Stopped: { label: "status.stopped", glyph: "○", color: "text-status-stopped" },
  Suspended: { label: "status.suspended", glyph: "⏸", color: "text-status-failed" },
  Failed: { label: "status.failed", glyph: "✕", color: "text-status-failed" },
  Crashed: { label: "status.crashed", glyph: "↻", color: "text-status-failed" },
};

export function StatusBadge({ phase }: { phase: GameServerPhase | string }) {
  const t = useT();
  const known = phaseConfig[phase];
  const config = known ?? { label: null, glyph: "○", color: "text-text-tertiary" };
  const label = known ? t(known.label) : phase || "—";

  return (
    <span className={`inline-flex items-center gap-1.5 font-sans text-xs font-semibold ${config.color}`}>
      <span aria-hidden>{config.glyph}</span>
      {label}
    </span>
  );
}
