import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Button } from "../components/ui/Button";
import { StatusBadge } from "../components/ui/StatusBadge";
import { ServerConsole } from "../components/console/ServerConsole";
import { ServerLogs } from "../components/console/ServerLogs";
import clsx from "clsx";

type Tab = "console" | "logs";

export function ServerDetailPage() {
  const { namespace = "", name = "" } = useParams();
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<Tab>("console");

  const { data: server, isLoading } = useQuery({
    queryKey: ["gameserver", namespace, name],
    queryFn: () => api.getGameServer(namespace, name),
    refetchInterval: 5000,
  });

  const setState = useMutation({
    mutationFn: (state: "Running" | "Stopped") => api.setGameServerState(namespace, name, state),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["gameserver", namespace, name] });
      void queryClient.invalidateQueries({ queryKey: ["gameservers"] });
    },
  });

  const deleteServer = useMutation({
    mutationFn: () => api.deleteGameServer(namespace, name),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["gameservers"] });
      navigate("/");
    },
  });

  if (isLoading || !server) {
    return <div className="font-sans text-sm text-text-secondary">Carregando…</div>;
  }

  const running = server.spec.state === "Running";

  return (
    <div className="flex h-full flex-col gap-5">
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-3">
            <div className="font-display text-2xl font-bold text-text-primary">{name}</div>
            <StatusBadge phase={server.status?.phase ?? ""} />
          </div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            {server.spec.eggRef.name} · {namespace} · {server.spec.storage.size}
          </div>
        </div>
        <div className="flex gap-2">
          <Button
            variant={running ? "secondary" : "primary"}
            disabled={setState.isPending}
            onClick={() => setState.mutate(running ? "Stopped" : "Running")}
          >
            {running ? "Parar" : "Iniciar"}
          </Button>
          {user?.isAdmin && (
            <Button
              variant="danger"
              disabled={deleteServer.isPending}
              onClick={() => {
                if (confirm(`Excluir ${name}? Isso remove o Pod e a PVC.`)) {
                  deleteServer.mutate();
                }
              }}
            >
              Excluir
            </Button>
          )}
        </div>
      </div>

      <div className="flex gap-1 border-b border-border">
        {(["console", "logs"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={clsx(
              "-mb-px border-b-2 px-3 py-2 font-sans text-sm font-medium capitalize",
              tab === t
                ? "border-primary text-text-primary"
                : "border-transparent text-text-tertiary hover:text-text-secondary",
            )}
          >
            {t === "console" ? "Console" : "Logs"}
          </button>
        ))}
      </div>

      <div className="min-h-0 grow">
        {tab === "console" ? (
          <ServerConsole namespace={namespace} name={name} />
        ) : (
          <ServerLogs namespace={namespace} name={name} />
        )}
      </div>
    </div>
  );
}
