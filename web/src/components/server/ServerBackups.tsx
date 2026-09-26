import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT, type TKey } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import type { BackupItem, BackupSettings, GameServer } from "../../lib/types";
import { Button } from "../ui/Button";
import { Link } from "react-router-dom";
import { Card } from "../ui/Card";
import { Field, Input } from "../ui/Input";
import { Badge, ListRow, RowActions } from "../ui/List";

const PHASE_KEY: Record<string, TKey> = {
  Pending: "backups.phasePending",
  Running: "backups.phaseRunning",
  Completed: "backups.phaseCompleted",
  Failed: "backups.phaseFailed",
  Deleting: "backups.phaseDeleting",
};

function fmt(iso?: string): string {
  return iso ? new Date(iso).toLocaleString() : "—";
}

function TargetCard({ org, name, server, settings, canManage }: { org: string; name: string; server: GameServer; settings: BackupSettings; canManage: boolean }) {
  const t = useT();
  const queryClient = useQueryClient();
  const saved = server.spec.backupTarget;
  const [conn, setConn] = useState<string | null>(null);
  const [bucket, setBucket] = useState<string | null>(null);
  const [prefix, setPrefix] = useState<string | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);

  const connection = conn ?? saved?.connection ?? "";
  const isPlatform = connection === "platform";
  const buckets = settings.connections.find((c) => c.name === connection)?.buckets ?? [];
  const chosenBucket = bucket ?? (connection === saved?.connection ? (saved?.bucket ?? "") : "");
  const bucketValue = buckets.includes(chosenBucket) ? chosenBucket : (buckets[0] ?? "");
  const prefixValue = prefix ?? (connection === saved?.connection ? (saved?.prefix ?? "") : "");

  const dirty =
    connection !== (saved?.connection ?? "") ||
    (!isPlatform && connection !== "" && (bucketValue !== (saved?.bucket ?? "") || prefixValue !== (saved?.prefix ?? "")));

  const save = useMutation({
    mutationFn: () =>
      api.updateGameServer(org, name, {
        backupTarget: connection === "" ? { connection: "" } : isPlatform ? { connection } : { connection, bucket: bucketValue, prefix: prefixValue },
      }),
    onSuccess: () => {
      setMessage({ ok: true, text: t("backups.targetSaved") });
      setConn(null);
      setBucket(null);
      setPrefix(null);
      void queryClient.invalidateQueries({ queryKey: ["gameserver", org, name] });
    },
    onError: (err) => setMessage({ ok: false, text: errorMessage(err, t("backups.saveFailed")) }),
  });

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("backups.targetTitle")}</div>
      <p className="font-prose text-sm text-text-secondary">{t("backups.targetHint")}</p>
      <div className="flex flex-wrap items-end gap-3">
        <Field label={t("backups.connection")} htmlFor="target-conn">
          <select
            id="target-conn"
            value={connection}
            disabled={!canManage}
            onChange={(e) => {
              setConn(e.target.value);
              setBucket(null);
              setPrefix(null);
            }}
            className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          >
            <option value="">{t("backups.chooseConnection")}</option>
            {settings.platform.available && <option value="platform">{t("backups.destPlatform")}</option>}
            {settings.connections.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
              </option>
            ))}
          </select>
        </Field>
        {connection !== "" && !isPlatform && (
          <>
            <Field label={t("backups.bucketLabel")} htmlFor="target-bucket">
              <select
                id="target-bucket"
                value={bucketValue}
                disabled={!canManage}
                onChange={(e) => setBucket(e.target.value)}
                className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
              >
                {buckets.map((b) => (
                  <option key={b} value={b}>
                    {b}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={t("backups.prefixLabel")} htmlFor="target-prefix">
              <Input id="target-prefix" value={prefixValue} disabled={!canManage} placeholder={name} onChange={(e) => setPrefix(e.target.value)} />
            </Field>
          </>
        )}
        {canManage && (
          <Button
            disabled={!dirty || save.isPending}
            onClick={() => {
              setMessage(null);
              save.mutate();
            }}
          >
            {t("backups.saveTarget")}
          </Button>
        )}
      </div>
      {isPlatform && <p className="font-prose text-xs text-text-tertiary">{t("backups.platformFixed")}</p>}
      {settings.connections.length === 0 && !settings.platform.available && (
        <p className="font-prose text-sm text-text-secondary">
          {canManage ? (
            <>
              {t("backups.noConnectionsYet")}{" "}
              <Link to="/settings" className="text-primary-text underline">
                {t("backups.openSettings")}
              </Link>
            </>
          ) : (
            t("backups.askAnAdmin")
          )}
        </p>
      )}
      {message && <div className={`font-sans text-sm ${message.ok ? "text-primary-text" : "text-status-failed"}`}>{message.text}</div>}
    </Card>
  );
}

function BackupRow({
  item,
  canManage,
  stopped,
  onRestore,
  onDelete,
  busy,
}: {
  item: BackupItem;
  canManage: boolean;
  stopped: boolean;
  onRestore: () => void;
  onDelete: () => void;
  busy: boolean;
}) {
  const t = useT();
  const restoring = item.restore && (item.restore.phase === "" || item.restore.phase === "Pending" || item.restore.phase === "Running");
  const canRestore = canManage && item.phase === "Completed" && !item.deleting && stopped && !restoring;
  const phase = item.deleting ? "Deleting" : item.phase || "Pending";

  return (
    <ListRow>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate font-mono text-xs text-text-primary">{item.name}</span>
          <Badge>{item.destination === "platform" ? t("backups.destPlatform") : item.destination}</Badge>
          <span className={`font-sans text-xs ${phase === "Failed" ? "text-status-failed" : "text-text-secondary"}`}>{t(PHASE_KEY[phase] ?? "backups.phasePending")}</span>
        </div>
        <div className="mt-0.5 font-sans text-xs text-text-tertiary">
          {t("backups.colCreated")} {fmt(item.createdAt)}
          {item.bucket && item.destination !== "platform" && ` · ${item.bucket}/${item.prefix ?? ""}`}
          {item.expiresAt && ` · ${t("backups.colExpires")} ${fmt(item.expiresAt)}`}
          {restoring && ` · ${t("backups.restoreRunning")}`}
          {item.restore?.phase === "Completed" && ` · ${t("backups.restoreDone")} ${fmt(item.restore.createdAt)}`}
          {item.restore?.phase === "Failed" && ` · ${t("backups.restoreFailed")}`}
          {item.quiesce && ` · ${t("backups.notQuiesced")}`}
          {item.resumeError && (
            <span className="text-status-failed" title={item.resumeError}>{` · ${t("backups.resumeFailed")}`}</span>
          )}
        </div>
      </div>
      {canManage && (
        <RowActions>
          <Button variant="secondary" disabled={!canRestore || busy} title={!stopped ? t("backups.restoreNeedsStopped") : undefined} onClick={onRestore}>
            {t("backups.restore")}
          </Button>
          <Button variant="ghost" disabled={item.deleting || busy} onClick={onDelete}>
            {t("backups.delete")}
          </Button>
        </RowActions>
      )}
    </ListRow>
  );
}

export function ServerBackups({ org, name, server, canManage }: { org: string; name: string; server: GameServer; canManage: boolean }) {
  const t = useT();
  const queryClient = useQueryClient();
  const stopped = server.spec.state === "Stopped";
  const [error, setError] = useState<string | null>(null);

  const { data: settings } = useQuery({ queryKey: ["backup-settings", org], queryFn: () => api.getBackupSettings(org) });
  const { data: list, error: listError } = useQuery({
    queryKey: ["backups", org, name],
    queryFn: () => api.listBackups(org, name),
    refetchInterval: 5000,
  });

  const hasTarget = !!server.spec.backupTarget?.connection;
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["backups", org, name] });
  const onError = (err: unknown) => setError(errorMessage(err, t("backups.actionFailed")));
  const create = useMutation({
    mutationFn: () => api.createBackup(org, name),
    onSuccess: refresh,
    onError: (err) => setError(errorMessage(err, t("backups.createFailed"))),
  });
  const restore = useMutation({ mutationFn: (b: string) => api.restoreBackup(org, name, b), onSuccess: refresh, onError });
  const remove = useMutation({ mutationFn: (b: string) => api.deleteBackup(org, name, b), onSuccess: refresh, onError });
  const busy = restore.isPending || remove.isPending;

  const limits = settings?.platform.limits;
  const items = list?.items ?? [];

  return (
    <div className="flex max-w-3xl flex-col gap-5">
      {settings && <TargetCard org={org} name={name} server={server} settings={settings} canManage={canManage} />}

      <Card className="flex flex-col gap-3 p-5">
        {canManage && (
          <div className="flex flex-wrap items-center gap-3">
            <Button
              disabled={!hasTarget || create.isPending}
              onClick={() => {
                setError(null);
                create.mutate();
              }}
            >
              {create.isPending ? t("backups.creating") : t("backups.create")}
            </Button>
            {!hasTarget && <span className="font-prose text-sm text-text-secondary">{t("backups.noTarget")}</span>}
          </div>
        )}
        {limits && server.spec.backupTarget?.connection === "platform" && list && (
          <p className="font-sans text-xs text-text-tertiary">
            {t("backups.usage", { server: list.usage.server, max: limits.maxPerServer, org: list.usage.org, orgMax: limits.maxPerOrg })}{" "}
            {t("backups.retention", { days: limits.retentionDays })}
          </p>
        )}
        {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
      </Card>

      <Card className="divide-y divide-border">
        {listError && <div className="px-4 py-3 font-sans text-sm text-status-failed">{errorMessage(listError, t("backups.loadFailed"))}</div>}
        {!listError && items.length === 0 && <div className="px-4 py-3 font-prose text-sm text-text-tertiary">{t("backups.empty")}</div>}
        {items.map((item) => (
          <BackupRow
            key={item.name}
            item={item}
            canManage={canManage}
            stopped={stopped}
            busy={busy}
            onRestore={() => {
              setError(null);
              if (confirm(t("backups.restoreConfirm", { name: item.name }))) restore.mutate(item.name);
            }}
            onDelete={() => {
              setError(null);
              if (confirm(t("backups.deleteConfirm", { name: item.name }))) remove.mutate(item.name);
            }}
          />
        ))}
      </Card>

    </div>
  );
}
