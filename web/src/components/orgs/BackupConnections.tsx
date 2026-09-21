import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import type { BackupConnection, BackupSettings } from "../../lib/types";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { Field, Input } from "../ui/Input";

export function BackupConnections({ org, settings, canManage }: { org: string; settings: BackupSettings; canManage: boolean }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [buckets, setBuckets] = useState("");
  const [accessKey, setAccessKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["backup-settings", org] });
    void queryClient.invalidateQueries({ queryKey: ["backups", org] });
  };
  const close = () => {
    setEditing(null);
    setAccessKey("");
    setSecretKey("");
  };
  const open = (c?: BackupConnection) => {
    setEditing(c?.name ?? "");
    setName(c?.name ?? "");
    setEndpoint(c?.endpoint ?? "");
    setBuckets((c?.buckets ?? []).join(", "));
    setAccessKey("");
    setSecretKey("");
    setMessage(null);
  };

  const isNew = editing === "";
  const save = useMutation({
    mutationFn: () =>
      api.putBackupConnection(org, name, {
        endpoint,
        buckets: buckets.split(/[\s,]+/).filter(Boolean),
        ...(accessKey || secretKey ? { accessKey, secretKey } : {}),
      }),
    onSuccess: () => {
      setMessage({ ok: true, text: t("backups.connSaved") });
      close();
      refresh();
    },
    onError: (err) => setMessage({ ok: false, text: errorMessage(err, t("backups.saveFailed")) }),
  });
  const remove = useMutation({
    mutationFn: (n: string) => api.deleteBackupConnection(org, n),
    onSuccess: refresh,
    onError: (err) => setMessage({ ok: false, text: errorMessage(err, t("backups.saveFailed")) }),
  });

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("backups.connectionsTitle")}</div>
      <p className="font-prose text-sm text-text-secondary">{t("backups.connectionsHint")}</p>
      {settings.connections.length === 0 && editing === null && (
        <div className="font-prose text-sm text-text-tertiary">{t("backups.noConnections")}</div>
      )}
      {settings.connections.map((c) => (
        <div key={c.name} className="flex flex-wrap items-center justify-between gap-2 border border-border px-3 py-2">
          <div className="min-w-0">
            <div className="font-mono text-xs text-text-primary">{c.name}</div>
            <div className="break-all font-sans text-xs text-text-tertiary">
              {c.endpoint || "AWS"} · {c.buckets.join(", ")}
            </div>
          </div>
          {canManage && (
            <div className="flex gap-2">
              <Button type="button" variant="secondary" onClick={() => open(c)}>
                {t("backups.edit")}
              </Button>
              <Button
                type="button"
                variant="ghost"
                disabled={remove.isPending}
                onClick={() => {
                  setMessage(null);
                  if (confirm(t("backups.removeConnectionConfirm", { name: c.name }))) remove.mutate(c.name);
                }}
              >
                {t("backups.removeConnection")}
              </Button>
            </div>
          )}
        </div>
      ))}

      {canManage && editing === null && (
        <div>
          <Button type="button" variant="secondary" onClick={() => open()}>
            {t("backups.newConnection")}
          </Button>
        </div>
      )}

      {canManage && editing !== null && (
        <form
          className="flex flex-col gap-3 border-t border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault();
            setMessage(null);
            save.mutate();
          }}
        >
          <Field label={t("backups.connName")} htmlFor="conn-name">
            <Input id="conn-name" value={name} disabled={!isNew} required pattern="[a-z0-9]([a-z0-9\-]{0,30}[a-z0-9])?" onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t("backups.endpoint")} htmlFor="conn-endpoint">
            <Input id="conn-endpoint" value={endpoint} placeholder="https://s3.example.com" onChange={(e) => setEndpoint(e.target.value)} />
          </Field>
          <Field label={t("backups.bucketsLabel")} htmlFor="conn-buckets">
            <Input id="conn-buckets" value={buckets} required onChange={(e) => setBuckets(e.target.value)} />
          </Field>
          <Field label={t("backups.accessKey")} htmlFor="conn-access">
            <Input id="conn-access" type="password" autoComplete="off" value={accessKey} required={isNew} onChange={(e) => setAccessKey(e.target.value)} />
          </Field>
          <Field label={t("backups.secretKey")} htmlFor="conn-secret">
            <Input id="conn-secret" type="password" autoComplete="off" value={secretKey} required={isNew} onChange={(e) => setSecretKey(e.target.value)} />
            {!isNew && <span className="font-prose text-xs text-text-tertiary">{t("backups.keepKeysHint")}</span>}
          </Field>
          <div className="flex gap-2">
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? t("backups.saving") : t("backups.save")}
            </Button>
            <Button type="button" variant="ghost" onClick={close}>
              {t("backups.cancelEdit")}
            </Button>
          </div>
        </form>
      )}
      {message && <div className={`font-sans text-sm ${message.ok ? "text-primary-text" : "text-status-failed"}`}>{message.text}</div>}
    </Card>
  );
}
