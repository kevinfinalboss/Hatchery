import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import { serverTitle } from "../../lib/gameserver";
import { Card } from "../ui/Card";
import { StatusBadge } from "../ui/StatusBadge";

export function OrgServers({ slug }: { slug: string }) {
  const t = useT();
  const { data, error } = useQuery({ queryKey: ["gameservers", slug], queryFn: () => api.listGameServers(slug), refetchInterval: 5000 });
  const servers = data?.items ?? [];

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("orgs.serversTitle")}</div>
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("dashboard.loadFailed"))}</div>}
      {!error && data && servers.length === 0 && <div className="font-prose text-sm text-text-tertiary">{t("orgs.noServers")}</div>}
      <div className="divide-y divide-border">
        {servers.map((s) => (
          <Link
            key={s.metadata.name}
            to={`/orgs/${slug}/servers/${s.metadata.name}`}
            className="flex items-center justify-between gap-3 py-2 transition-colors hover:text-primary-text"
          >
            <span className="min-w-0 truncate font-sans text-sm text-text-primary">{serverTitle(s)}</span>
            <span className="flex shrink-0 items-center gap-3">
              <span className="hidden font-mono text-xs text-text-tertiary sm:inline">{s.spec.eggRef.name}</span>
              <StatusBadge phase={s.status?.phase ?? ""} />
            </span>
          </Link>
        ))}
      </div>
    </Card>
  );
}
