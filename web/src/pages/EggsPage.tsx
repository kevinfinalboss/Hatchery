import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useT } from "../lib/i18n";
import { useAuth } from "../lib/auth";
import { atLeast, useOrg } from "../lib/org";
import { errorMessage } from "../lib/errors";
import type { EggEntry, EggSpec } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { Badge, ListRow, RowActions } from "../components/ui/List";
import { Filtered } from "../components/ui/Filtered";

const TEMPLATE: EggSpec = {
  image: "",
  startCommand: "",
  variables: [],
  ports: [{ name: "game", containerPort: 25565, protocol: "TCP", default: true }],
};

function EggEditor({
  initialName,
  initialSpec,
  lockName,
  saving,
  error,
  onSave,
  onCancel,
}: {
  initialName: string;
  initialSpec: EggSpec;
  lockName: boolean;
  saving: boolean;
  error: string | null;
  onSave: (name: string, spec: EggSpec) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [name, setName] = useState(initialName);
  const [json, setJson] = useState(JSON.stringify(initialSpec, null, 2));
  const [parseError, setParseError] = useState<string | null>(null);

  function submit() {
    try {
      const spec = JSON.parse(json) as EggSpec;
      setParseError(null);
      onSave(name.trim(), spec);
    } catch (e) {
      setParseError(t("eggs.invalidJson", { message: e instanceof Error ? e.message : String(e) }));
    }
  }

  return (
    <Card className="flex flex-col gap-4 p-5">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
        className="flex flex-col gap-4"
      >
        <Field label={t("common.name")} htmlFor="egg-name">
          <Input id="egg-name" value={name} onChange={(e) => setName(e.target.value)} disabled={lockName} required />
        </Field>
        <Field label={t("eggs.specLabel")} htmlFor="egg-spec">
          <textarea
            id="egg-spec"
            value={json}
            onChange={(e) => setJson(e.target.value)}
            spellCheck={false}
            rows={14}
            className="w-full rounded-lg border border-border-strong bg-surface px-3 py-2 font-mono text-xs text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          />
        </Field>
        <div className="font-sans text-xs text-text-tertiary">
          {t("eggs.warning")}
        </div>
        <div className="flex gap-2">
          <Button type="submit" disabled={saving}>
            {saving ? t("common.saving") : t("common.save")}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        </div>
      </form>
      {(parseError || error) && <div className="font-sans text-sm text-status-failed">{parseError ?? error}</div>}
    </Card>
  );
}

export function EggsPage() {
  const t = useT();
  const { user } = useAuth();
  const { current } = useOrg();
  const queryClient = useQueryClient();
  const org = current?.slug ?? "";
  const canWritePrivate = atLeast(current?.role, "admin");
  const canWriteCatalog = !!user?.isAdmin;

  const [editing, setEditing] = useState<null | "new-private" | "new-catalog" | EggEntry>(null);
  const [error, setError] = useState<string | null>(null);

  const { data: eggs, isLoading } = useQuery({
    queryKey: ["eggs", org],
    queryFn: () => api.listOrgEggs(org),
    enabled: !!org,
  });

  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["eggs", org] });
  const done = () => {
    setEditing(null);
    setError(null);
    refresh();
  };
  const fail = (err: unknown) => setError(errorMessage(err, t("eggs.saveFailed")));

  const save = useMutation({
    mutationFn: async ({ name, spec }: { name: string; spec: EggSpec }) => {
      if (editing === "new-private") return api.createOrgEgg(org, name, spec);
      if (editing === "new-catalog") return api.createCatalogEgg(name, spec);
      if (editing && editing.scope === "Catalog") return api.updateCatalogEgg(editing.name, spec);
      if (editing) return api.updateOrgEgg(org, editing.name, spec);
    },
    onSuccess: done,
    onError: fail,
  });

  const remove = useMutation({
    mutationFn: (egg: EggEntry) => (egg.scope === "Catalog" ? api.deleteCatalogEgg(egg.name) : api.deleteOrgEgg(org, egg.name)),
    onSuccess: done,
    onError: (err) => setError(errorMessage(err, t("eggs.deleteFailed"))),
  });

  if (!current) return <div className="font-sans text-sm text-text-secondary">{t("common.selectOrg")}</div>;

  const canWrite = (egg: EggEntry) => (egg.scope === "Catalog" ? canWriteCatalog : canWritePrivate);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="font-display text-2xl font-bold text-text-primary">{t("eggs.title")}</div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            {t("eggs.subtitle", { org: current.name })}
          </div>
        </div>
        {editing === null && (
          <div className="flex gap-2">
            {canWritePrivate && <Button onClick={() => setEditing("new-private")}>{t("eggs.newPrivate")}</Button>}
            {canWriteCatalog && (
              <Button variant="secondary" onClick={() => setEditing("new-catalog")}>
                {t("eggs.newCatalog")}
              </Button>
            )}
          </div>
        )}
      </div>

      {editing !== null && (
        <EggEditor
          key={typeof editing === "string" ? editing : `${editing.scope}:${editing.name}`}
          initialName={typeof editing === "string" ? "" : editing.name}
          initialSpec={typeof editing === "string" ? TEMPLATE : editing.spec}
          lockName={typeof editing !== "string"}
          saving={save.isPending}
          error={error}
          onSave={(name, spec) => {
            setError(null);
            save.mutate({ name, spec });
          }}
          onCancel={() => {
            setEditing(null);
            setError(null);
          }}
        />
      )}

      {editing === null && error && <div className="font-sans text-sm text-status-failed">{error}</div>}
      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}

      <Filtered items={eggs ?? []} text={(egg) => egg.name} placeholder={t("eggs.filterPlaceholder")}>
        {(items) => (
          <Card className="divide-y divide-border">
            {items.map((egg) => (
              <ListRow key={`${egg.scope}:${egg.name}`}>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-display text-[15px] font-semibold text-text-primary">{egg.name}</span>
                    <Badge>{egg.scope === "Catalog" ? t("eggs.scopeCatalog") : t("eggs.scopePrivate")}</Badge>
                  </div>
                  <div className="truncate font-mono text-xs text-text-tertiary">{egg.spec.image}</div>
                </div>
                {canWrite(egg) && (
                  <RowActions>
                    <Button variant="secondary" onClick={() => setEditing(egg)}>
                      {t("common.edit")}
                    </Button>
                    <Button
                      variant="ghost"
                      disabled={remove.isPending}
                      onClick={() => {
                        if (confirm(t("eggs.deleteConfirm", { name: egg.name }))) remove.mutate(egg);
                      }}
                    >
                      {t("common.delete")}
                    </Button>
                  </RowActions>
                )}
              </ListRow>
            ))}
            {!isLoading && items.length === 0 && (
              <div className="px-4 py-6 font-prose text-sm text-text-tertiary">{t("eggs.empty")}</div>
            )}
          </Card>
        )}
      </Filtered>
    </div>
  );
}
