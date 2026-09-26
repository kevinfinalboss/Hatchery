import { useT } from "../../lib/i18n";
import type { OrgQuota } from "../../lib/types";
import { Field, Input } from "../ui/Input";

export function QuotaFields({ quota, onChange }: { quota: OrgQuota; onChange: (q: OrgQuota) => void }) {
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
      <div className="flex flex-col gap-2 border-t border-border pt-3">
        <label className="flex items-center gap-2 font-sans text-sm text-text-primary">
          <input
            type="checkbox"
            checked={!!quota.backups}
            onChange={(e) =>
              onChange({ ...quota, backups: e.target.checked ? { maxPerServer: 3, maxPerOrg: 10, retentionDays: 7 } : undefined })
            }
          />
          {t("orgs.backupsEnable")}
        </label>
        {quota.backups && (
          <div className="flex flex-wrap gap-3">
            <Field label={t("orgs.backupsMaxServer")} htmlFor="q-bk-server">
              <Input
                id="q-bk-server"
                type="number"
                min={0}
                value={quota.backups.maxPerServer}
                onChange={(e) => onChange({ ...quota, backups: { ...quota.backups!, maxPerServer: Number(e.target.value) } })}
                required
                className="w-28"
              />
            </Field>
            <Field label={t("orgs.backupsMaxOrg")} htmlFor="q-bk-org">
              <Input
                id="q-bk-org"
                type="number"
                min={0}
                value={quota.backups.maxPerOrg}
                onChange={(e) => onChange({ ...quota, backups: { ...quota.backups!, maxPerOrg: Number(e.target.value) } })}
                required
                className="w-28"
              />
            </Field>
            <Field label={t("orgs.backupsRetention")} htmlFor="q-bk-days">
              <Input
                id="q-bk-days"
                type="number"
                min={1}
                value={quota.backups.retentionDays}
                onChange={(e) => onChange({ ...quota, backups: { ...quota.backups!, retentionDays: Number(e.target.value) } })}
                required
                className="w-28"
              />
            </Field>
          </div>
        )}
      </div>
      <Field label={t("orgs.auditRetention")} htmlFor="q-audit-days">
        <Input
          id="q-audit-days"
          type="number"
          min={7}
          max={3650}
          value={quota.auditRetentionDays ?? ""}
          onChange={(e) =>
            onChange({ ...quota, auditRetentionDays: e.target.value === "" ? undefined : Number(e.target.value) })
          }
          className="w-28"
        />
        <span className="font-prose text-xs text-text-tertiary">{t("orgs.auditRetentionHint")}</span>
      </Field>
      <Field label={t("orgs.extraRegistries")} htmlFor="q-registries">
        <textarea
          id="q-registries"
          rows={3}
          value={(quota.extraImageRegistries ?? []).join("\n")}
          onChange={(e) => onChange({ ...quota, extraImageRegistries: e.target.value.split("\n") })}
          spellCheck={false}
          className="w-full rounded-lg border border-border-strong bg-surface px-3 py-2 font-mono text-xs text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
        />
        <span className="font-prose text-xs text-text-tertiary">{t("orgs.extraRegistriesHint")}</span>
      </Field>
    </>
  );
}
