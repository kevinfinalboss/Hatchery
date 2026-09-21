import { Link } from "react-router-dom";
import { Card } from "./ui/Card";
import { StatusBadge } from "./ui/StatusBadge";
import { useT } from "../lib/i18n";
import type { GameServer } from "../lib/types";
import { serverTitle } from "../lib/gameserver";

export function ServerCard({ org, server }: { org: string; server: GameServer }) {
  const t = useT();
  const { metadata, spec, status } = server;
  const base = `/orgs/${org}/servers/${metadata.name}`;

  return (
    <Card className="flex flex-col gap-3 p-4 transition-colors hover:border-border-strong">
      <div className="flex items-start justify-between gap-2">
        <Link to={base} className="min-w-0">
          <div className="truncate font-display text-[15px] font-semibold text-text-primary">{serverTitle(server)}</div>
          <div className="truncate font-sans text-xs text-text-tertiary">{spec.eggRef.name}</div>
        </Link>
        <StatusBadge phase={status?.phase ?? ""} />
      </div>
      <div className="flex gap-4 font-sans text-xs text-text-secondary">
        <span>
          {t("serverCard.storage")} <span className="text-text-primary">{spec.storage.size}</span>
        </span>
        <span>
          {t("serverCard.state")} <span className="text-text-primary">{spec.state}</span>
        </span>
      </div>
      <Link to={`${base}?s=console`} className="font-sans text-xs text-text-secondary hover:text-primary-text">
        {t("serverCard.console")}
      </Link>
    </Card>
  );
}
