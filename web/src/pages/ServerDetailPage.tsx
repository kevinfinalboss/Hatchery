import { useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { api } from "../lib/api";
import { atLeast, roleOf, useOrg } from "../lib/org";
import { useRestartServer, useSetServerState } from "../lib/serverActions";
import { Button } from "../components/ui/Button";
import { useT } from "../lib/i18n";
import { StatusBadge } from "../components/ui/StatusBadge";
import { ServerConsole } from "../components/console/ServerConsole";
import { FileManager } from "../components/files/FileManager";
import { ServerMetrics } from "../components/server/ServerMetrics";
import { ServerSettings } from "../components/server/ServerSettings";
import { restartRequired, serverTitle } from "../lib/gameserver";
import { errorMessage } from "../lib/errors";
import type { UpdateGameServerRequest } from "../lib/types";

type Section = "console" | "metrics" | "files" | "settings";

const SECTIONS: {
  id: Section;
  label: "server.sectionConsole" | "server.sectionMetrics" | "server.sectionFiles" | "server.sectionSettings";
}[] = [
  { id: "console", label: "server.sectionConsole" },
  { id: "metrics", label: "server.sectionMetrics" },
  { id: "files", label: "server.sectionFiles" },
  { id: "settings", label: "server.sectionSettings" },
];

function sectionOf(raw: string | null): Section {
  return raw === "metrics" || raw === "files" || raw === "settings" ? raw : "console";
}

export function ServerDetailPage() {
  const t = useT();
  const { org = "", name = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const section = sectionOf(params.get("s"));
  const { orgs } = useOrg();
  const canDelete = atLeast(roleOf(orgs, org), "admin");
  const canEdit = canDelete; // admin and owner may edit; members only read
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setState = useSetServerState();
  const restart = useRestartServer();

  const { data: server, isLoading } = useQuery({
    queryKey: ["gameserver", org, name],
    queryFn: () => api.getGameServer(org, name),
    refetchInterval: 5000,
  });

  const { data: eggs } = useQuery({ queryKey: ["eggs", org], queryFn: () => api.listOrgEggs(org) });
  const egg = server && eggs?.find((e) => e.name === server.spec.eggRef.name && e.scope === (server.spec.eggRef.scope ?? "Namespace"));

  const [saveMessage, setSaveMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const update = useMutation({
    mutationFn: (body: UpdateGameServerRequest) => api.updateGameServer(org, name, body),
    onSuccess: () => {
      setSaveMessage({ ok: true, text: t("server.saved") });
      void queryClient.invalidateQueries({ queryKey: ["gameserver", org, name] });
      void queryClient.invalidateQueries({ queryKey: ["gameservers", org] });
      void queryClient.invalidateQueries({ queryKey: ["quota", org] });
    },
    onError: (err) => setSaveMessage({ ok: false, text: errorMessage(err, t("server.saveFailed")) }),
  });

  const deleteServer = useMutation({
    mutationFn: () => api.deleteGameServer(org, name),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["gameservers", org] });
      navigate("/");
    },
  });

  if (isLoading || !server) {
    return <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>;
  }

  const desiredRunning = server.spec.state === "Running";
  const podRunning = server.status?.phase === "Running";

  return (
    <div className="flex h-full flex-col gap-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <div className="font-display text-xl font-bold text-text-primary">{serverTitle(server)}</div>
            <StatusBadge phase={server.status?.phase ?? ""} />
          </div>
          <div className="mt-0.5 font-sans text-xs text-text-secondary">
            {server.spec.eggRef.name} · {org} · {server.spec.storage.size}
            {server.spec.displayName ? ` · ${name}` : ""}
          </div>
        </div>
        <div className="flex gap-2">
          {desiredRunning && (
            <Button
              variant="secondary"
              disabled={restart.isPending}
              onClick={() => {
                if (confirm(t("server.restartConfirm", { name: serverTitle(server) }))) restart.mutate({ org, name });
              }}
            >
              {t("server.restart")}
            </Button>
          )}
          <Button
            variant={desiredRunning ? "secondary" : "primary"}
            disabled={setState.isPending}
            onClick={() => setState.mutate({ org, name, state: desiredRunning ? "Stopped" : "Running" })}
          >
            {desiredRunning ? t("server.stop") : t("server.start")}
          </Button>
        </div>
      </div>

      {restartRequired(server) && desiredRunning && (
        <div className="flex flex-wrap items-center justify-between gap-3 border border-border-strong bg-surface px-4 py-2.5 font-sans text-sm text-text-primary">
          <span>{t("server.restartPending")}</span>
          <Button
            variant="secondary"
            disabled={restart.isPending}
            onClick={() => {
              if (confirm(t("server.restartConfirm", { name: serverTitle(server) }))) restart.mutate({ org, name });
            }}
          >
            {t("server.restartNow")}
          </Button>
        </div>
      )}

      <div className="flex min-h-0 grow flex-col gap-4 md:flex-row md:gap-6">
        <nav className="flex shrink-0 gap-1 border-b border-border pb-2 md:w-40 md:flex-col md:border-b-0 md:pb-0">
          {SECTIONS.map((s) => (
            <button
              key={s.id}
              onClick={() => setParams({ s: s.id }, { replace: true })}
              className={clsx(
                "flex items-center gap-2 px-3 py-1.5 text-left font-sans text-sm",
                section === s.id ? "bg-surface-hover text-primary-text" : "text-text-secondary hover:text-text-primary",
              )}
            >
              <span aria-hidden className="w-2">
                {section === s.id ? ">" : ""}
              </span>
              {t(s.label)}
            </button>
          ))}
        </nav>

        <div className="min-h-0 min-w-0 grow">
          {section === "console" ? (
            <ServerConsole org={org} name={name} running={podRunning} />
          ) : section === "metrics" ? (
            <ServerMetrics org={org} name={name} server={server} running={podRunning} />
          ) : section === "files" ? (
            <FileManager org={org} name={name} />
          ) : (
            <ServerSettings
              org={org}
              server={server}
              egg={egg}
              canEdit={canEdit}
              saving={update.isPending}
              saveMessage={saveMessage}
              onSave={(body) => {
                setSaveMessage(null);
                update.mutate(body);
              }}
              canDelete={canDelete}
              deleting={deleteServer.isPending}
              onDelete={() => deleteServer.mutate()}
            />
          )}
        </div>
      </div>
    </div>
  );
}
