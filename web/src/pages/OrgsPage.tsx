import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useI18n, useT } from "../lib/i18n";
import { errorMessage } from "../lib/errors";
import type { OrgQuota, OrgSummary } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { Badge } from "../components/ui/List";
import { QuotaFields } from "../components/orgs/QuotaFields";
import { DEFAULT_QUOTA } from "../lib/orgQuota";
import { Filtered } from "../components/ui/Filtered";

function CreateOrgForm({ onClose }: { onClose: () => void }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [quota, setQuota] = useState<OrgQuota>(DEFAULT_QUOTA);
  const [error, setError] = useState<string | null>(null);

  const [inviteUrl, setInviteUrl] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => api.createOrg({ slug: slug.trim(), name: name.trim(), ownerEmail: owner.trim(), quota }),
    onSuccess: (org) => {
      void queryClient.invalidateQueries({ queryKey: ["orgs"] });
      if (org.inviteError) setError(org.inviteError);
      if (org.inviteUrl) setInviteUrl(org.inviteUrl);
      else if (!org.inviteError) onClose();
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
          <Input id="org-owner" type="email" value={owner} onChange={(e) => setOwner(e.target.value)} required />
        </Field>
        <div className="w-full font-sans text-xs text-text-tertiary">{t("orgs.ownerHelp")}</div>
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
      {inviteUrl && (
        <div className="flex flex-col gap-2">
          <div className="font-sans text-sm text-text-secondary">{t("orgs.ownerInviteLink")}</div>
          <code className="break-all rounded border border-border bg-canvas px-2 py-1 font-mono text-xs text-text-secondary">{inviteUrl}</code>
          <div>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t("common.close")}
            </Button>
          </div>
        </div>
      )}
    </Card>
  );
}

function OrgRow({ org }: { org: OrgSummary }) {
  const t = useT();
  const { data: detail } = useQuery({ queryKey: ["org", org.slug], queryFn: () => api.getOrg(org.slug), refetchInterval: 10000 });

  return (
    <Link to={`/orgs/${org.slug}`} className="group flex flex-col gap-1 px-4 py-3 transition-colors hover:bg-surface-hover">
      <div className="flex items-center gap-2">
        <span className="font-display text-[15px] font-semibold text-text-primary">{org.name}</span>
        <span className="font-mono text-xs text-text-tertiary">{org.slug}</span>
        {detail && <Badge>{detail.phase}</Badge>}
        <span aria-hidden className="ml-auto font-sans text-text-tertiary group-hover:text-primary-text">
          →
        </span>
      </div>
      {detail?.quota && (
        <div className="font-mono text-xs text-text-secondary">
          {t("orgs.quotaSummary", {
            cpu: detail.quota.cpu,
            memory: detail.quota.memory,
            storage: detail.quota.storage,
            max: detail.quota.maxGameServers,
          })}
        </div>
      )}
    </Link>
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
