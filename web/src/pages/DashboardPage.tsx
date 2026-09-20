import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { ServerCard } from "../components/ServerCard";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";

function CreateServerForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();
  const { data: eggs } = useQuery({ queryKey: ["eggs"], queryFn: api.listEggs });
  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [eggName, setEggName] = useState("");
  const [size, setSize] = useState("2Gi");
  const [eulaAccepted, setEulaAccepted] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const eggOptions = eggs?.items ?? [];
  const selectedEgg = eggOptions.find((egg) => egg.metadata.name === eggName);
  const needsEula = selectedEgg?.spec.variables?.some((v) => v.name === "EULA") ?? false;

  const create = useMutation({
    mutationFn: () =>
      api.createGameServer({
        name,
        namespace,
        spec: {
          eggRef: { name: eggName },
          state: "Running",
          storage: { size },
          ...(needsEula ? { variables: [{ name: "EULA", value: "TRUE" }] } : {}),
        },
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["gameservers"] });
      onClose();
    },
    onError: (err) => setError(err instanceof Error ? err.message : "Falha ao criar servidor"),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">Novo servidor</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          create.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label="Nome" htmlFor="new-name">
          <Input id="new-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <Field label="Namespace" htmlFor="new-namespace">
          <Input
            id="new-namespace"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            required
          />
        </Field>
        <Field label="Egg" htmlFor="new-egg">
          <select
            id="new-egg"
            value={eggName}
            onChange={(e) => {
              setEggName(e.target.value);
              setEulaAccepted(false);
            }}
            required
            className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          >
            <option value="" disabled>
              Selecione…
            </option>
            {eggOptions.map((egg) => (
              <option key={egg.metadata.name} value={egg.metadata.name}>
                {egg.metadata.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Storage" htmlFor="new-size">
          <Input id="new-size" value={size} onChange={(e) => setSize(e.target.value)} required />
        </Field>

        {needsEula && (
          <label className="flex w-full items-start gap-2 rounded-lg border border-border-strong bg-surface p-3 font-sans text-sm text-text-secondary">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={eulaAccepted}
              onChange={(e) => setEulaAccepted(e.target.checked)}
            />
            <span>
              Li e aceito a{" "}
              <a
                href="https://aka.ms/MinecraftEULA"
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary underline"
              >
                EULA da Mojang
              </a>
              . Sem isso o servidor de Minecraft não inicia (fica em crash loop).
            </span>
          </label>
        )}

        <div className="flex gap-2">
          <Button type="submit" disabled={create.isPending || (needsEula && !eulaAccepted)}>
            {create.isPending ? "Criando…" : "Criar"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
        </div>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

export function DashboardPage() {
  const { user } = useAuth();
  const [creating, setCreating] = useState(false);
  const { data, isLoading, error } = useQuery({
    queryKey: ["gameservers"],
    queryFn: api.listGameServers,
    refetchInterval: 5000,
  });

  const servers = data?.items ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="font-display text-2xl font-bold text-text-primary">Servidores</div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            {servers.length} servidor{servers.length === 1 ? "" : "es"}
          </div>
        </div>
        {user?.isAdmin && !creating && (
          <Button onClick={() => setCreating(true)}>+ Novo servidor</Button>
        )}
      </div>

      {creating && <CreateServerForm onClose={() => setCreating(false)} />}

      {isLoading && <div className="font-sans text-sm text-text-secondary">Carregando…</div>}
      {error && (
        <div className="font-sans text-sm text-status-failed">
          {error instanceof Error ? error.message : "Falha ao carregar servidores"}
        </div>
      )}

      {!isLoading && servers.length === 0 && (
        <div className="font-sans text-sm text-text-tertiary">
          Nenhum servidor {user?.isAdmin ? "ainda — crie o primeiro acima." : "com acesso concedido a você."}
        </div>
      )}

      <div className="flex flex-wrap gap-5">
        {servers.map((server) => (
          <ServerCard key={`${server.metadata.namespace}/${server.metadata.name}`} server={server} />
        ))}
      </div>
    </div>
  );
}
