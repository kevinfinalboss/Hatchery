import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useI18n, useT } from "../lib/i18n";
import { errorMessage } from "../lib/errors";
import type { OrgQuota, OrgSummary } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { Badge, RowActions } from "../components/ui/List";
import { Filtered } from "../components/ui/Filtered";

const DEFAULT_QUOTA: OrgQuota = { cpu: "4", memory: "8Gi", storage: "50Gi", maxGameServers: 3 };

function QuotaFields({ quota, onChange }: { quota: OrgQuota; onChange: (q: OrgQuota) => void }) {
  const t = useT();
  return (
    <>
      <Field label="CPU" htmlFor="q-cpu">
        <Input id="q-cpu" value={quota.cpu} onChange={(e) => onChange({ ...quota, cpu: e.target.value })} required className="w-24" />
      </Field>
      <Field label={t("orgs.quotaMemory")} htmlFor="q-mem">
        <Input id="q-mem" value={quota.memory} onChange={(e) => onChange({ ...quota, memory: e.target.value })} required className="w-28" />
      </Field>
      <Field label="Storage" htmlFor="q-sto">
        <Input id="q-sto" value={quota.storage} onChange={(e) => onChange({ ...quota, storage: e.target.value })} required className="w-28" />
      </Field>
      <Field label={t("orgs.quotaMaxServers")} htmlFor="q-max">
        <Input
          id="q-max"
          type="number"
          min={0}
          value={quota.maxGameServers}
          onChange={(e) => onChange({ ...quota, maxGameServers: Number(e.target.value) })}
          required
          className="w-28"
        />
      </Field>
    </>
  );
}

function CreateOrgForm({ onClose }: { onClose: () => void }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [quota, setQuota] = useState<OrgQuota>(DEFAULT_QUOTA);
  const [error, setError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.createOrg({ slug: slug.trim(), name: name.trim(), ownerUsername: owner.trim(), quota }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["orgs"] });
      onClose();
    },
    onError: (err) => setError(errorMessage(err, t("orgs.createFailed"))),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">{t("orgs.newTitle")}</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          create.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label={t("orgs.slug")} htmlFor="org-slug">
          <Input id="org-slug" value={slug} onChange={(e) => setSlug(e.target.value)} pattern="[a-z0-9]([\-a-z0-9]{0,30}[a-z0-9])?" required />
        </Field>
        <Field label={t("common.name")} htmlFor="org-name">
          <Input id="org-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <Field label={t("orgs.owner")} htmlFor="org-owner">
          <Input id="org-owner" value={owner} onChange={(e) => setOwner(e.target.value)} required />
        </Field>
        <QuotaFields quota={quota} onChange={setQuota} />
        <div className="flex gap-2">
          <Button type="submit" disabled={create.isPending}>
            {create.isPending ? t("common.creating") : t("common.create")}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
        </div>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

function OrgRow({ org }: { org: OrgSummary }) {
  const t = useT();
  const queryClient = useQueryClient();
  const { data: detail } = useQuery({ queryKey: ["org", org.slug], queryFn: () => api.getOrg(org.slug), refetchInterval: 10000 });
  const [editing, setEditing] = useState(false);
  const [quota, setQuota] = useState<OrgQuota>(DEFAULT_QUOTA);
  const [error, setError] = useState<string | null>(null);

  const saveQuota = useMutation({
    mutationFn: () => api.updateOrgQuota(org.slug, quota),
    onSuccess: () => {
      setEditing(false);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["org", org.slug] });
    },
    onError: (err) => setError(errorMessage(err, t("orgs.quotaUpdateFailed"))),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteOrg(org.slug),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["orgs"] }),
    onError: (err) => setError(errorMessage(err, t("orgs.deleteFailed"))),
  });

  return (
    <div className="group flex flex-col gap-3 px-4 py-3 transition-colors hover:bg-surface-hover">
      <div className="flex items-center justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-display text-[15px] font-semibold text-text-primary">{org.name}</span>
            <span className="font-mono text-xs text-text-tertiary">{org.slug}</span>
            {detail && <Badge>{detail.phase}</Badge>}
          </div>
          {detail?.quota && !editing && (
            <div className="mt-1 font-mono text-xs text-text-secondary">
              {t("orgs.quotaSummary", {
                cpu: detail.quota.cpu,
                memory: detail.quota.memory,
                storage: detail.quota.storage,
                max: detail.quota.maxGameServers,
              })}
            </div>
          )}
        </div>
        <RowActions>
          <Button
            variant="secondary"
            onClick={() => {
              if (detail?.quota) setQuota(detail.quota);
              setEditing((v) => !v);
            }}
            disabled={!detail?.quota}
          >
            {t("orgs.quota")}
          </Button>
          <Button
            variant="ghost"
            disabled={remove.isPending}
            onClick={() => {
              if (confirm(t("orgs.deleteConfirm", { name: org.name }))) remove.mutate();
            }}
          >
            {t("common.delete")}
          </Button>
        </RowActions>
      </div>

      {editing && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            saveQuota.mutate();
          }}
          className="flex flex-wrap items-end gap-4"
        >
          <QuotaFields quota={quota} onChange={setQuota} />
          <Button type="submit" disabled={saveQuota.isPending}>
            {saveQuota.isPending ? t("common.saving") : t("orgs.saveQuota")}
          </Button>
        </form>
      )}
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </div>
  );
}

export function OrgsPage() {
  const { t, plural } = useI18n();
  const [creating, setCreating] = useState(false);
  const { data: orgs, isLoading, error } = useQuery({ queryKey: ["orgs"], queryFn: api.listOrgs });

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="font-display text-2xl font-bold text-text-primary">{t("orgs.title")}</div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            {plural("orgs.count", (orgs ?? []).length)}
          </div>
        </div>
        {!creating && <Button onClick={() => setCreating(true)}>{t("orgs.newButton")}</Button>}
      </div>

      {creating && <CreateOrgForm onClose={() => setCreating(false)} />}

      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("orgs.loadFailed"))}</div>}

      <Filtered items={orgs ?? []} text={(o) => `${o.name} ${o.slug}`} placeholder={t("orgs.filterPlaceholder")}>
        {(items) => (
          <Card className="divide-y divide-border">
            {items.map((o) => (
              <OrgRow key={o.slug} org={o} />
            ))}
          </Card>
        )}
      </Filtered>
    </div>
  );
}
