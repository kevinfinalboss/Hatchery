import { useT } from "../lib/i18n";
import type { OnlinePlayers } from "../lib/types";

export function PlayersCount({ players }: { players: OnlinePlayers }) {
  const t = useT();
  const label = t("serverCard.players", { online: players.online, max: players.max });
  return (
    <span title={label} aria-label={label} className="inline-flex items-center gap-1 font-sans text-xs tabular-nums text-text-secondary">
      <svg aria-hidden="true" viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="currentColor">
        <circle cx="8" cy="5" r="3" />
        <path d="M2 14c0-3.3 2.7-5 6-5s6 1.7 6 5z" />
      </svg>
      {players.online}/{players.max}
    </span>
  );
}
