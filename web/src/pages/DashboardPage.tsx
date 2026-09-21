import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useT, type TKey } from "../lib/i18n";
import { useOrg, atLeast } from "../lib/org";
import { usePersistedChoice } from "../lib/usePersistedChoice";
import type { GameServer } from "../lib/types";
import { serverTitle } from "../lib/gameserver";
import { ServerCard } from "../components/ServerCard";
import { ServerList } from "../components/ServerList";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { type DashboardView, ViewToggle } from "../components/ui/ViewToggle";

type StatusFilter = "all" | "running" | "installing" | "stopped" | "failed";

const STATUS_FILTERS: { value: StatusFilter; label: TKey }[] = [
  { value: "all", label: "dashboard.filterAll" },
  { value: "running", label: "status.running" },
  { value: "installing", label: "status.installing" },
  { value: "stopped", label: "status.stopped" },
  { value: "failed", label: "status.failed" },
];

function matchesStatus(server: GameServer, filter: StatusFilter): boolean {
  const phase = server.status?.phase ?? "";
  switch (filter) {
    case "all":
      return true;
    case "running":
      return phase === "Running";
    case "installing":
      return phase === "Installing" || phase === "Pending" || phase === "";
    case "stopped":
      return phase === "Stopped" || phase === "Stopping";
    case "failed":
      return phase === "Failed";
  }
}

export function DashboardPage() {
  const t = useT();
  const { current, loading: orgsLoading } = useOrg();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [view, setView] = usePersistedChoice<DashboardView>("hatchery_dashboard_view", ["list", "cards"], "list");
  const org = current?.slug ?? "";

  const { data, isLoading, error } = useQuery({
    queryKey: ["gameservers", org],
    queryFn: () => api.listGameServers(org),
    refetchInterval: 5000,
    enabled: !!org,
  });
  const { data: orgDetail } = useQuery({
    queryKey: ["org", org],
    queryFn: () => api.getOrg(org),
    refetchInterval: 10000,
    enabled: !!org,
  });

  if (orgsLoading) return <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>;
  if (!current) {
    return (
      <div className="font-prose text-sm text-text-secondary">
        {t("dashboard.noOrg")}
      </div>
    );
  }

  const servers = data?.items ?? [];
  const needle = query.trim().toLowerCase();
  const visible = servers.filter(
    (s) =>
      matchesStatus(s, statusFilter) &&
      (!needle || s.metadata.name.toLowerCase().includes(needle) || serverTitle(s).toLowerCase().includes(needle)),
  );
  const canCreate = atLeast(current.role, "admin");
  const provisioning = orgDetail !== undefined && !orgDetail.ready;
  const quota = orgDetail?.quota;

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between gap-4">
        <div>
          <div className="font-display text-xl font-bold text-text-primary">
            {t("dashboard.title")} <span className="font-normal text-text-tertiary">{servers.length}</span>
          </div>
          {quota && (
            <div className="mt-0.5 font-sans text-xs text-text-secondary">
              {t("dashboard.quota", { max: quota.maxGameServers, cpu: quota.cpu, memory: quota.memory, storage: quota.storage })}
            </div>
          )}
        </div>
        {canCreate && (
          <Button onClick={() => navigate("/new-server")} disabled={provisioning}>
            {t("dashboard.newButton")}
          </Button>
        )}
      </div>

      {provisioning && (
        <div className="border border-border-strong bg-surface p-3 font-prose text-sm text-text-secondary">
          {t("dashboard.provisioning", { phase: orgDetail?.phase ?? "" })}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("dashboard.filterPlaceholder")}
          aria-label={t("dashboard.filterAria")}
          className="w-full max-w-xs"
        />
        <div className="flex gap-1">
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.value}
              onClick={() => setStatusFilter(f.value)}
              aria-pressed={statusFilter === f.value}
              className={
                statusFilter === f.value
                  ? "px-2 py-1 font-sans text-xs text-primary-text"
                  : "px-2 py-1 font-sans text-xs text-text-tertiary hover:text-text-primary"
              }
            >
              {t(f.label)}
            </button>
          ))}
        </div>
        <div className="ml-auto">
          <ViewToggle value={view} onChange={setView} />
        </div>
      </div>

      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {error && (
        <div className="font-sans text-sm text-status-failed">
          {error instanceof Error ? error.message : t("dashboard.loadFailed")}
        </div>
      )}

      {!isLoading && servers.length === 0 && (
        <div className="font-prose text-sm text-text-tertiary">
          {canCreate ? t("dashboard.emptyCanCreate") : t("dashboard.emptyReadOnly")}
        </div>
      )}
      {!isLoading && servers.length > 0 && visible.length === 0 && (
        <div className="font-prose text-sm text-text-tertiary">{t("dashboard.noMatch")}</div>
      )}

      {visible.length > 0 &&
        (view === "list" ? (
          <ServerList org={org} servers={visible} />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {visible.map((server) => (
              <ServerCard key={server.metadata.name} org={org} server={server} />
            ))}
          </div>
        ))}
    </div>
  );
}
