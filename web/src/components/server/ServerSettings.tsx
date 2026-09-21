import { type ReactNode, useState } from "react";
import type { EggEntry, GameServer, UpdateGameServerRequest } from "../../lib/types";
import { useT } from "../../lib/i18n";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { Field, Input } from "../ui/Input";
import { type ByteQuantity, joinBytes, splitBytes } from "../../lib/quantity";
import { ByteInput, CpuInput, ImageSelect, VariableFields, editableVariables, variableProblem } from "./ServerFields";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 px-4 py-2.5">
      <span className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{label}</span>
      <span className="min-w-0 break-all text-right font-sans text-sm text-text-primary">{children}</span>
    </div>
  );
}

const FALLBACK_MEM: ByteQuantity = { value: 2, unit: "Gi" };

export function ServerSettings({
  org,
  server,
  egg,
  canEdit,
  canDelete,
  deleting,
  saving,
  saveMessage,
  onSave,
  onDelete,
}: {
  org: string;
  server: GameServer;
  egg?: EggEntry;
  canEdit: boolean;
  canDelete: boolean;
  deleting: boolean;
  saving: boolean;
  saveMessage: { ok: boolean; text: string } | null;
  onSave: (body: UpdateGameServerRequest) => void;
  onDelete: () => void;
}) {
  const t = useT();
  const { spec, status, metadata } = server;
  const variables = editableVariables(egg?.spec);
  const limits = spec.resources?.limits;
  const images = egg?.spec.images ?? [];

  const current = {
    name: spec.displayName ?? "",
    image: spec.imageName || images[0]?.name || "",
    values: Object.fromEntries(
      variables.map((v) => [v.name, spec.variables?.find((o) => o.name === v.name)?.value ?? v.default ?? ""]),
    ) as Record<string, string>,
    cpu: limits?.cpu ?? "",
    mem: splitBytes(limits?.memory, FALLBACK_MEM),
  };

  const [nameEdit, setNameEdit] = useState<string | null>(null);
  const [imageEdit, setImageEdit] = useState<string | null>(null);
  const [valueEdits, setValueEdits] = useState<Record<string, string>>({});
  const [cpuEdit, setCpuEdit] = useState<string | null>(null);
  const [memEdit, setMemEdit] = useState<ByteQuantity | null>(null);

  const name = nameEdit ?? current.name;
  const image = imageEdit ?? current.image;
  const values = { ...current.values, ...valueEdits };
  const cpu = cpuEdit ?? current.cpu;
  const mem = memEdit ?? current.mem;

  const nameChanged = nameEdit !== null && nameEdit !== current.name;
  const imageChanged = imageEdit !== null && imageEdit !== current.image;
  const varsChanged = Object.keys(valueEdits).some((k) => valueEdits[k] !== current.values[k]);
  const cpuChanged = cpuEdit !== null && cpuEdit !== current.cpu;
  const memChanged = memEdit !== null && joinBytes(memEdit) !== joinBytes(current.mem);
  const dirty = nameChanged || imageChanged || varsChanged || cpuChanged || memChanged;
  const valid = variables.every((v) => variableProblem(v, values[v.name] ?? "") === null) && cpu !== "" && Number.isFinite(mem.value);

  const save = () => {
    const body: UpdateGameServerRequest = {};
    if (nameChanged) body.displayName = name.trim();
    if (imageChanged) body.imageName = image;
    if (varsChanged) {
      // The override list is replaced as a whole: keep what is already set (including EULA, which
      // this screen does not show) and apply the edits on top.
      const next = new Map((spec.variables ?? []).map((o) => [o.name, o.value]));
      for (const [k, v] of Object.entries(valueEdits)) next.set(k, v);
      body.variables = [...next].map(([n, v]) => ({ name: n, value: v }));
    }
    if (cpuChanged || memChanged) body.resources = { limits: { cpu, memory: joinBytes(mem) } };
    onSave(body);
  };

  return (
    <div className="flex max-w-2xl flex-col gap-5">
      <Card className="divide-y divide-border">
        <Row label={t("server.org")}>{org}</Row>
        <Row label="id">{metadata.name}</Row>
        <Row label="egg">
          {spec.eggRef.name}
          {spec.eggRef.scope ? ` (${spec.eggRef.scope === "Catalog" ? t("server.scopeCatalog") : t("server.scopePrivate")})` : ""}
        </Row>
        <Row label={t("server.desiredState")}>{spec.state}</Row>
        <Row label="pod">{status?.podName ?? "—"}</Row>
      </Card>

      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        {!canEdit && <div className="font-prose text-sm text-text-tertiary">{t("server.readOnlyHint")}</div>}

        <Field label={t("server.name")} htmlFor="set-name">
          <Input id="set-name" value={name} maxLength={64} disabled={!canEdit} onChange={(e) => setNameEdit(e.target.value)} required />
        </Field>

        {images.length > 1 && (
          <Field label={t("dashboard.image")} htmlFor="set-image">
            <ImageSelect id="set-image" images={images} value={image} disabled={!canEdit} onChange={setImageEdit} />
          </Field>
        )}

        <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.variables")}</div>
        {variables.length === 0 && <div className="font-prose text-sm text-text-tertiary">{t("server.noVariables")}</div>}
        <VariableFields
          variables={variables}
          values={values}
          disabled={!canEdit}
          idPrefix="set-var"
          onChange={(n, v) => setValueEdits((cur) => ({ ...cur, [n]: v }))}
        />

        <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.resources")}</div>
        <Field label={t("dashboard.cpu")} htmlFor="set-cpu">
          <CpuInput id="set-cpu" value={cpu} disabled={!canEdit} onChange={setCpuEdit} />
        </Field>
        <Field label={t("dashboard.memory")} htmlFor="set-mem">
          <ByteInput id="set-mem" value={mem} disabled={!canEdit} onChange={setMemEdit} />
        </Field>
        <Field label={t("dashboard.disk")} htmlFor="set-disk">
          <Input id="set-disk" value={spec.storage.size} disabled readOnly />
          <span className="font-prose text-xs text-text-tertiary">{t("server.diskFixed")}</span>
        </Field>

        {canEdit && (
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={!dirty || !valid || saving}>
              {saving ? t("server.saving") : t("server.saveChanges")}
            </Button>
            {saveMessage && (
              <span className={`font-sans text-sm ${saveMessage.ok ? "text-primary-text" : "text-status-failed"}`}>{saveMessage.text}</span>
            )}
          </div>
        )}
      </form>

      {canDelete && (
        <div>
          <div className="mb-2 font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.dangerZone")}</div>
          <Button
            variant="danger"
            disabled={deleting}
            onClick={() => {
              if (confirm(t("server.deleteConfirm", { name: server.spec.displayName || metadata.name }))) onDelete();
            }}
          >
            {t("server.deleteServer")}
          </Button>
        </div>
      )}
    </div>
  );
}
