import { Link, useNavigate } from "react-router-dom";
import { useSetServerState } from "../lib/serverActions";
import { useT } from "../lib/i18n";
import { serverTitle } from "../lib/gameserver";
import type { GameServer } from "../lib/types";
import { Card } from "./ui/Card";
import { StatusBadge } from "./ui/StatusBadge";

const COLUMNS = "grid grid-cols-[1.4fr_1fr] gap-4 md:grid-cols-[1.4fr_1fr_1fr_1fr_4rem]";

function ServerRow({ org, server }: { org: string; server: GameServer }) {
  const navigate = useNavigate();
  const t = useT();
  const setState = useSetServerState();
  const { metadata, spec, status } = server;
  const path = `/orgs/${org}/servers/${metadata.name}`;
  const running = spec.state === "Running";

  return (
    <div
      className={`${COLUMNS} group cursor-pointer items-center px-4 py-3 transition-colors hover:bg-surface-hover`}
      onClick={() => navigate(path)}
    >
      <Link to={path} onClick={(e) => e.stopPropagation()} className="truncate font-sans text-sm font-semibold text-text-primary">
        {serverTitle(server)}
      </Link>
      <StatusBadge phase={status?.phase ?? ""} />
      <span className="hidden truncate font-sans text-sm text-text-secondary md:block">{spec.eggRef.name}</span>
      <span className="hidden font-sans text-sm text-text-secondary md:block">{spec.storage.size}</span>
      <button
        className="hidden text-right font-sans text-xs text-text-secondary hover:text-primary-text disabled:opacity-50 md:block md:opacity-0 md:group-hover:opacity-100 md:focus:opacity-100"
        disabled={setState.isPending}
        onClick={(e) => {
          e.stopPropagation();
          setState.mutate({ org, name: metadata.name, state: running ? "Stopped" : "Running" });
        }}
      >
        {running ? t("serverList.stop") : t("serverList.start")}
      </button>
    </div>
  );
}

export function ServerList({ org, servers }: { org: string; servers: GameServer[] }) {
  const t = useT();
  return (
    <Card className="divide-y divide-border">
      <div className={`${COLUMNS} px-4 py-2 font-sans text-xs uppercase tracking-wide text-text-tertiary`}>
        <span>{t("serverList.name")}</span>
        <span>{t("serverList.status")}</span>
        <span className="hidden md:block">{t("serverList.egg")}</span>
        <span className="hidden md:block">{t("serverList.storage")}</span>
        <span className="hidden md:block" />
      </div>
      {servers.map((server) => (
        <ServerRow key={server.metadata.name} org={org} server={server} />
      ))}
    </Card>
  );
}
