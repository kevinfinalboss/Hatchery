import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useT } from "../lib/i18n";
import { useOrg } from "../lib/org";
import type { EggEntry, EggSpec } from "../lib/types";
import { type ByteQuantity, formatBytes, joinBytes, parseBytes, parseCpu, splitBytes, toBytes } from "../lib/quantity";
import { ByteInput, CpuInput, ImageSelect, VariableFields, editableVariables, variableProblem } from "../components/server/ServerFields";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";

const DEFAULT_CPU = "1";
const DEFAULT_MEM: ByteQuantity = { value: 2, unit: "Gi" };
const DEFAULT_DISK: ByteQuantity = { value: 10, unit: "Gi" };

function slugPreview(name: string): string {
  const s = name
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!s) return "server";
  return /^[0-9]/.test(s) ? `server-${s}` : s;
}

function defaultValues(spec: EggSpec | undefined): Record<string, string> {
  return Object.fromEntries(editableVariables(spec).map((v) => [v.name, v.default ?? ""]));
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{title}</div>
      {children}
    </Card>
  );
}

function SummaryRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3 font-sans text-sm">
      <span className="text-text-tertiary">{label}</span>
      <span className="min-w-0 truncate text-right text-text-primary">{value}</span>
    </div>
  );
}

export function NewServerPage() {
  const t = useT();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { current } = useOrg();
  const org = current?.slug ?? "";

  const { data: eggs } = useQuery({ queryKey: ["eggs", org], queryFn: () => api.listOrgEggs(org), enabled: !!org });
  const { data: quota } = useQuery({ queryKey: ["quota", org], queryFn: () => api.getQuota(org), retry: false, enabled: !!org });
  const { data: orgDetail } = useQuery({ queryKey: ["org", org], queryFn: () => api.getOrg(org), enabled: !!org });

  const [displayName, setDisplayName] = useState("");
  const [eggChoice, setEggChoice] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [imageName, setImageName] = useState("");
  const [cpu, setCpu] = useState(DEFAULT_CPU);
  const [mem, setMem] = useState<ByteQuantity>(DEFAULT_MEM);
  const [disk, setDisk] = useState<ByteQuantity>(DEFAULT_DISK);
  const [eulaAccepted, setEulaAccepted] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const eggOptions: EggEntry[] = eggs ?? [];
  const selectedEgg = eggOptions.find((egg) => `${egg.scope}:${egg.name}` === eggChoice);
  const needsEula = selectedEgg?.spec.variables?.some((v) => v.name === "EULA") ?? false;
  const variables = editableVariables(selectedEgg?.spec);
  const images = selectedEgg?.spec.images ?? [];
  const variablesOk = variables.every((v) => variableProblem(v, values[v.name] ?? "") === null);
  const provisioning = orgDetail !== undefined && !orgDetail.ready;

  const chooseEgg = (choice: string) => {
    const egg = eggOptions.find((e) => `${e.scope}:${e.name}` === choice);
    const rec = egg?.spec.recommendedResources;
    setEggChoice(choice);
    setEulaAccepted(false);
    setImageName(egg?.spec.images?.[0]?.name ?? "");
    setValues(defaultValues(egg?.spec));
    setCpu(rec?.cpu ?? DEFAULT_CPU);
    setMem(splitBytes(rec?.memory, DEFAULT_MEM));
    setDisk(splitBytes(rec?.disk, DEFAULT_DISK));
  };

  const left = quota && {
    cpu: parseCpu(quota.limit.cpu) - parseCpu(quota.used.cpu),
    memory: parseBytes(quota.limit.memory) - parseBytes(quota.used.memory),
    disk: parseBytes(quota.limit.storage) - parseBytes(quota.used.storage),
  };
  const overQuota = !!left && (Number(cpu) > left.cpu || toBytes(mem) > left.memory || toBytes(disk) > left.disk);

  const create = useMutation({
    mutationFn: () => {
      const overrides = variables
        .filter((v) => (values[v.name] ?? "") !== (v.default ?? ""))
        .map((v) => ({ name: v.name, value: values[v.name] ?? "" }));
      if (needsEula) overrides.push({ name: "EULA", value: "TRUE" });
      return api.createGameServer(org, {
        spec: {
          displayName: displayName.trim(),
          eggRef: { name: selectedEgg!.name, scope: selectedEgg!.scope },
          ...(images.length > 1 ? { imageName } : {}),
          state: "Running",
          storage: { size: joinBytes(disk) },
          resources: { limits: { cpu, memory: joinBytes(mem) } },
          ...(overrides.length > 0 ? { variables: overrides } : {}),
        },
      });
    },
    onSuccess: (created) => {
      void queryClient.invalidateQueries({ queryKey: ["gameservers", org] });
      void queryClient.invalidateQueries({ queryKey: ["quota", org] });
      navigate(`/orgs/${org}/servers/${created.metadata.name}`);
    },
    onError: (err) => setError(err instanceof Error ? err.message : t("dashboard.createFailed")),
  });

  const canSubmit = !!selectedEgg && !create.isPending && !overQuota && variablesOk && !provisioning && (!needsEula || eulaAccepted);

  return (
    <div className="flex flex-col gap-5">
      <div>
        <Link to="/" className="font-sans text-xs text-text-secondary hover:text-primary-text">
          ← {t("dashboard.back")}
        </Link>
        <div className="mt-1 font-display text-xl font-bold text-text-primary">{t("dashboard.newServer")}</div>
      </div>

      {provisioning && (
        <div className="border border-border-strong bg-surface p-3 font-prose text-sm text-text-secondary">
          {t("dashboard.provisioning", { phase: orgDetail?.phase ?? "" })}
        </div>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          create.mutate();
        }}
        className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]"
      >
        <div className="flex min-w-0 flex-col gap-5">
          <Section title={t("dashboard.identitySection")}>
            <Field label={t("dashboard.displayName")} htmlFor="new-name">
              <Input id="new-name" value={displayName} maxLength={64} onChange={(e) => setDisplayName(e.target.value)} required />
              {displayName.trim() && (
                <span className="font-sans text-xs text-text-tertiary">{t("dashboard.slugPreview", { slug: slugPreview(displayName) })}</span>
              )}
            </Field>
            <Field label="Egg" htmlFor="new-egg">
              <select
                id="new-egg"
                value={eggChoice}
                onChange={(e) => chooseEgg(e.target.value)}
                required
                className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
              >
                <option value="" disabled>
                  {t("common.select")}
                </option>
                {eggOptions.map((egg) => (
                  <option key={`${egg.scope}:${egg.name}`} value={`${egg.scope}:${egg.name}`}>
                    {egg.name}
                    {egg.scope === "Catalog" ? t("dashboard.catalogSuffix") : t("dashboard.privateSuffix")}
                  </option>
                ))}
              </select>
            </Field>
            {images.length > 1 && (
              <Field label={t("dashboard.image")} htmlFor="new-image">
                <ImageSelect id="new-image" images={images} value={imageName} onChange={setImageName} />
              </Field>
            )}
          </Section>

          {selectedEgg && variables.length > 0 && (
            <Section title={t("dashboard.settingsSection")}>
              <VariableFields
                variables={variables}
                values={values}
                onChange={(name, value) => setValues((cur) => ({ ...cur, [name]: value }))}
                idPrefix="new-var"
              />
            </Section>
          )}

          <Section title={t("dashboard.resourcesSection")}>
            <Field label={t("dashboard.cpu")} htmlFor="new-cpu">
              <CpuInput id="new-cpu" value={cpu} onChange={setCpu} />
            </Field>
            <Field label={t("dashboard.memory")} htmlFor="new-mem">
              <ByteInput id="new-mem" value={mem} onChange={setMem} />
            </Field>
            <Field label={t("dashboard.disk")} htmlFor="new-disk">
              <ByteInput id="new-disk" value={disk} onChange={setDisk} />
            </Field>
          </Section>

          {needsEula && (
            <label className="flex w-full items-start gap-2 border border-border-strong bg-surface p-3 font-prose text-sm text-text-secondary">
              <input type="checkbox" className="mt-0.5" checked={eulaAccepted} onChange={(e) => setEulaAccepted(e.target.checked)} />
              <span>
                {t("dashboard.eulaAccept")}{" "}
                <a href="https://aka.ms/MinecraftEULA" target="_blank" rel="noopener noreferrer" className="text-primary-text underline">
                  {t("dashboard.eulaLink")}
                </a>
                {t("dashboard.eulaRest")}
              </span>
            </label>
          )}
        </div>

        <aside className="lg:sticky lg:top-4">
          <Card className="flex flex-col gap-3 p-5">
            <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("dashboard.summary")}</div>
            <SummaryRow label={t("dashboard.displayName")} value={displayName.trim() || "—"} />
            <SummaryRow label="id" value={displayName.trim() ? slugPreview(displayName) : "—"} />
            <SummaryRow label="Egg" value={selectedEgg?.name ?? t("dashboard.noEggYet")} />
            {images.length > 1 && <SummaryRow label={t("dashboard.image")} value={imageName} />}
            <SummaryRow label={t("dashboard.cpu")} value={cpu || "—"} />
            <SummaryRow label={t("dashboard.memory")} value={Number.isFinite(mem.value) ? joinBytes(mem) : "—"} />
            <SummaryRow label={t("dashboard.disk")} value={Number.isFinite(disk.value) ? joinBytes(disk) : "—"} />
            {left && (
              <div className={`font-sans text-xs ${overQuota ? "text-status-failed" : "text-text-tertiary"}`}>
                {overQuota
                  ? t("dashboard.overQuota")
                  : t("dashboard.quotaLeft", { cpu: +left.cpu.toFixed(2), memory: formatBytes(left.memory), disk: formatBytes(left.disk) })}
              </div>
            )}
            {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
            <div className="flex gap-2 pt-1">
              <Button type="submit" disabled={!canSubmit}>
                {create.isPending ? t("common.creating") : t("common.create")}
              </Button>
              <Button type="button" variant="ghost" onClick={() => navigate("/")}>
                {t("common.cancel")}
              </Button>
            </div>
          </Card>
        </aside>
      </form>
    </div>
  );
}
