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
  reinstalling,
  onReinstall,
  isPlatformAdmin,
  suspending,
  onSuspend,
  onUnsuspend,
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
  reinstalling: boolean;
  onReinstall: () => void;
  isPlatformAdmin: boolean;
  suspending: boolean;
  onSuspend: (reason: string) => void;
  onUnsuspend: () => void;
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
    cmd: spec.startCommand ?? "",
    values: Object.fromEntries(
      variables.map((v) => [v.name, spec.variables?.find((o) => o.name === v.name)?.value ?? v.default ?? ""]),
    ) as Record<string, string>,
    cpu: limits?.cpu ?? "",
    mem: splitBytes(limits?.memory, FALLBACK_MEM),
  };

  const [nameEdit, setNameEdit] = useState<string | null>(null);
  const [imageEdit, setImageEdit] = useState<string | null>(null);
  const [cmdEdit, setCmdEdit] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [valueEdits, setValueEdits] = useState<Record<string, string>>({});
  const [cpuEdit, setCpuEdit] = useState<string | null>(null);
  const [memEdit, setMemEdit] = useState<ByteQuantity | null>(null);
  const [exposeEdit, setExposeEdit] = useState<boolean | null>(null);
  const [autoRestartEdit, setAutoRestartEdit] = useState<boolean | null>(null);

  const name = nameEdit ?? current.name;
  const image = imageEdit ?? current.image;
  const eggCmd = egg?.spec.startCommand ?? "";
  const cmd = cmdEdit ?? (current.cmd || eggCmd);
  const storedCmd = cmd.trim() === "" || cmd === eggCmd ? "" : cmd;
  const customCmd = storedCmd !== "";
  const values = { ...current.values, ...valueEdits };
  const cpu = cpuEdit ?? current.cpu;
  const mem = memEdit ?? current.mem;
  const expose = exposeEdit ?? (spec.publicExposure?.enabled ?? false);
  const exposureReason = status?.conditions?.find((c) => c.type === "PublicExposureReady")?.reason;
  const exposeChanged = exposeEdit !== null && exposeEdit !== (spec.publicExposure?.enabled ?? false);
  const autoRestart = autoRestartEdit ?? (spec.autoRestart ?? true);
  const autoRestartChanged = autoRestartEdit !== null && autoRestartEdit !== (spec.autoRestart ?? true);

  const nameChanged = nameEdit !== null && nameEdit !== current.name;
  const cmdChanged = egg !== undefined && cmdEdit !== null && storedCmd !== current.cmd;
  const imageChanged = imageEdit !== null && imageEdit !== current.image;
  const varsChanged = Object.keys(valueEdits).some((k) => valueEdits[k] !== current.values[k]);
  const cpuChanged = cpuEdit !== null && cpuEdit !== current.cpu;
  const memChanged = memEdit !== null && joinBytes(memEdit) !== joinBytes(current.mem);
  const dirty = nameChanged || imageChanged || cmdChanged || varsChanged || cpuChanged || memChanged || exposeChanged || autoRestartChanged;
  const valid = variables.every((v) => variableProblem(v, values[v.name] ?? "") === null) && cpu !== "" && Number.isFinite(mem.value);

  const save = () => {
    const body: UpdateGameServerRequest = {};
    if (nameChanged) body.displayName = name.trim();
    if (imageChanged) body.imageName = image;
    if (cmdChanged) body.startCommand = storedCmd;
    if (varsChanged) {
      // The override list is replaced as a whole: keep what is already set (including EULA, which
      // this screen does not show) and apply the edits on top.
      const next = new Map((spec.variables ?? []).map((o) => [o.name, o.value]));
      for (const [k, v] of Object.entries(valueEdits)) next.set(k, v);
      body.variables = [...next].map(([n, v]) => ({ name: n, value: v }));
    }
    if (cpuChanged || memChanged) body.resources = { limits: { cpu, memory: joinBytes(mem) } };
    if (exposeChanged) body.publicExposureEnabled = expose;
    if (autoRestartChanged) body.autoRestart = autoRestart;
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

        <Field label={t("server.startCommand")} htmlFor="set-cmd">
          <div className="flex gap-2">
            <Input
              id="set-cmd"
              className="min-w-0 grow font-mono text-xs"
              value={cmd}
              disabled={!canEdit || egg === undefined}
              maxLength={4096}
              onChange={(e) => setCmdEdit(e.target.value)}
            />
            {canEdit && customCmd && (
              <Button type="button" variant="ghost" onClick={() => setCmdEdit(eggCmd)}>
                {t("server.startCommandReset")}
              </Button>
            )}
          </div>
          <span className={`font-sans text-xs ${customCmd ? "text-primary-text" : "text-text-tertiary"}`}>
            {customCmd ? t("server.startCommandCustom") : t("server.startCommandDefault")}
          </span>
          <span className="font-prose text-xs text-text-tertiary">{t("server.startCommandHint")}</span>
          {(egg?.spec.variables?.length ?? 0) > 0 && (
            <span className="break-all font-mono text-xs text-text-tertiary">
              {t("server.startCommandVars", { vars: (egg?.spec.variables ?? []).map((v) => `{{${v.name}}}`).join(" ") })}
            </span>
          )}
        </Field>

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

        <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.autoRestart")}</div>
        <label className="flex items-center gap-2 font-prose text-sm text-text-primary">
          <input
            type="checkbox"
            checked={autoRestart}
            disabled={!canEdit}
            onChange={(e) => setAutoRestartEdit(e.target.checked)}
          />
          {t("server.autoRestartEnable")}
        </label>
        <span className="font-prose text-xs text-text-tertiary">{t("server.autoRestartHint")}</span>

        <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.publicExposure")}</div>
        <label className="flex items-center gap-2 font-prose text-sm text-text-primary">
          <input
            type="checkbox"
            checked={expose}
            disabled={!canEdit}
            onChange={(e) => setExposeEdit(e.target.checked)}
          />
          {t("server.publicExposureEnable")}
        </label>
        {status?.publicExposure?.ports && status.publicExposure.ports.length > 0 && (
          <div className="font-mono text-xs text-text-secondary">
            {status.publicExposure.ports.map((p) => (
              <div key={p.name}>
                {p.name}: {status.publicExposure?.host}:{p.port}
              </div>
            ))}
          </div>
        )}
        {expose && (!status?.publicExposure?.ports || status.publicExposure.ports.length === 0) && (
          <span className="font-prose text-xs text-text-tertiary">
            {exposureReason === "NotConfigured"
              ? t("server.publicExposureNotConfigured")
              : exposureReason === "PoolExhausted"
                ? t("server.publicExposurePoolExhausted")
                : t("server.publicExposurePending")}
          </span>
        )}

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

      {(canDelete || isPlatformAdmin) && (
        <div>
          <div className="mb-2 font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.dangerZone")}</div>
          {isPlatformAdmin && (
            <div className="mb-3 flex flex-col gap-2">
              {spec.suspended ? (
                <div className="flex flex-wrap items-center gap-3">
                  <span className="font-prose text-sm text-text-secondary">{spec.suspendReason}</span>
                  <Button
                    variant="secondary"
                    disabled={suspending}
                    onClick={() => {
                      if (confirm(t("server.unsuspendConfirm", { name: spec.displayName || metadata.name }))) onUnsuspend();
                    }}
                  >
                    {t("server.unsuspend")}
                  </Button>
                </div>
              ) : (
                <div className="flex flex-wrap items-end gap-2">
                  <Field label={t("server.suspendReasonLabel")} htmlFor="suspend-reason">
                    <Input id="suspend-reason" value={reason} maxLength={256} onChange={(e) => setReason(e.target.value)} />
                  </Field>
                  <Button
                    variant="danger"
                    disabled={suspending || reason.trim() === ""}
                    onClick={() => {
                      if (confirm(t("server.suspendConfirm", { name: spec.displayName || metadata.name }))) onSuspend(reason.trim());
                    }}
                  >
                    {t("server.suspend")}
                  </Button>
                </div>
              )}
            </div>
          )}
          {canDelete && (<>
          <div className="mb-3 flex flex-col gap-1.5">
            <div>
              <Button
                variant="secondary"
                disabled={reinstalling}
                onClick={() => {
                  const key = spec.state === "Running" ? "server.reinstallConfirmRunning" : "server.reinstallConfirmStopped";
                  if (confirm(t(key, { name: server.spec.displayName || metadata.name }))) onReinstall();
                }}
              >
                {t("server.reinstall")}
              </Button>
            </div>
            <span className="font-prose text-xs text-text-tertiary">{t("server.reinstallHint")}</span>
          </div>
          <Button
            variant="danger"
            disabled={deleting}
            onClick={() => {
              if (confirm(t("server.deleteConfirm", { name: server.spec.displayName || metadata.name }))) onDelete();
            }}
          >
            {t("server.deleteServer")}
          </Button>
          </>)}
        </div>
      )}
    </div>
  );
}
