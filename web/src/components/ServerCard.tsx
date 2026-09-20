import { Link } from "react-router-dom";
import { Card } from "./ui/Card";
import { StatusBadge } from "./ui/StatusBadge";
import type { GameServer } from "../lib/types";

export function ServerCard({ org, server }: { org: string; server: GameServer }) {
  const { metadata, spec, status } = server;

  return (
    <Link to={`/orgs/${org}/servers/${metadata.name}`}>
      <Card className="flex w-[300px] flex-col gap-3 p-[18px] transition-colors hover:border-border-strong">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="truncate font-display text-[15px] font-semibold text-text-primary">
              {metadata.name}
            </div>
            <div className="truncate font-sans text-xs text-text-tertiary">{spec.eggRef.name}</div>
          </div>
          <StatusBadge phase={status?.phase ?? ""} />
        </div>
        <div className="flex gap-4 font-mono text-[11px] text-text-secondary">
          <span>
            Storage <span className="text-text-primary">{spec.storage.size}</span>
          </span>
          <span>
            Estado <span className="text-text-primary">{spec.state}</span>
          </span>
        </div>
      </Card>
    </Link>
  );
}
