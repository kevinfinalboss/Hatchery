import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT, type TKey } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import { ALL_PERMISSIONS, PRESETS } from "../../lib/permissions";
import { serverTitle } from "../../lib/gameserver";
import type { GameServer, Permission, ServerGrant } from "../../lib/types";
import { Button } from "../ui/Button";
import { Modal } from "../ui/Modal";

const ALL_TARGET = "*";

type GrantMap = Map<string, Set<Permission>>;

interface PermGroup {
  title: TKey;
  items: { perm: Permission; label: TKey }[];
}

const GROUPS: PermGroup[] = [
  {
    title: "permissions.groupConsole",
    items: [
      { perm: "console.read", label: "permissions.consoleRead" },
      { perm: "console.write", label: "permissions.consoleWrite" },
    ],
  },
  {
    title: "permissions.groupPower",
    items: [{ perm: "power", label: "permissions.power" }],
  },
  {
    title: "permissions.groupFiles",
    items: [
      { perm: "files.read", label: "permissions.filesRead" },
      { perm: "files.write", label: "permissions.filesWrite" },
    ],
  },
  {
    title: "permissions.groupBackups",
    items: [
      { perm: "backups.read", label: "permissions.backupsRead" },
      { perm: "backups.manage", label: "permissions.backupsManage" },
    ],
  },
  {
    title: "permissions.groupSchedules",
    items: [{ perm: "schedules", label: "permissions.schedules" }],
  },
];

function grantsToMap(grants: ServerGrant[]): GrantMap {
  const map: GrantMap = new Map();
  for (const g of grants) map.set(g.gameserver, new Set(g.permissions));
  return map;
}

function mapToGrants(map: GrantMap): ServerGrant[] {
  return Array.from(map.entries()).map(([gameserver, permissions]) => ({
    gameserver,
    permissions: ALL_PERMISSIONS.filter((p) => permissions.has(p)),
  }));
}

function PermissionFields({ perms, onChange }: { perms: Set<Permission>; onChange: (next: Set<Permission>) => void }) {
  const t = useT();

  function toggle(p: Permission) {
    const next = new Set(perms);
    if (next.has(p)) next.delete(p);
    else next.add(p);
    onChange(next);
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="secondary" onClick={() => onChange(new Set(PRESETS.readOnly))}>
          {t("members.presetReadOnly")}
        </Button>
        <Button type="button" variant="secondary" onClick={() => onChange(new Set(PRESETS.moderator))}>
          {t("members.presetModerator")}
        </Button>
        <Button type="button" variant="secondary" onClick={() => onChange(new Set(PRESETS.full))}>
          {t("members.presetFull")}
        </Button>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {GROUPS.map((group) => (
          <div key={group.title} className="flex flex-col gap-1.5">
            <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t(group.title)}</div>
            {group.items.map((item) => (
              <label key={item.perm} className="flex items-center gap-2 font-sans text-sm text-text-primary">
                <input type="checkbox" checked={perms.has(item.perm)} onChange={() => toggle(item.perm)} />
                {t(item.label)}
              </label>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

export function MemberPermissionsEditor({
  org,
  userId,
  username,
  onClose,
  showNoAccessHint,
}: {
  org: string;
  userId: number;
  username: string;
  onClose: () => void;
  showNoAccessHint?: boolean;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [map, setMap] = useState<GrantMap | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ["member-permissions", org, userId],
    queryFn: () => api.getMemberPermissions(org, userId),
  });
  const { data: serverList } = useQuery({
    queryKey: ["gameservers", org],
    queryFn: () => api.listGameServers(org),
  });
  const servers: GameServer[] = serverList?.items ?? [];

  useEffect(() => {
    if (data) setMap(grantsToMap(data.grants));
  }, [data]);

  const save = useMutation({
    mutationFn: (grants: ServerGrant[]) => api.putMemberPermissions(org, userId, grants),
    onSuccess: (result) => {
      setMap(grantsToMap(result.grants));
      setMessage({ ok: true, text: t("members.saved") });
      void queryClient.invalidateQueries({ queryKey: ["member-permissions", org, userId] });
    },
    onError: (err) => setMessage({ ok: false, text: errorMessage(err, t("members.saveFailed")) }),
  });

  if (isLoading || !map) {
    return (
      <Modal title={t("members.permissionsTitle", { name: username })} onClose={onClose} wide>
        <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>
      </Modal>
    );
  }

  const mode: "all" | "chosen" = map.has(ALL_TARGET) ? "all" : "chosen";

  function switchMode(next: "all" | "chosen") {
    if (next === mode) return;
    if (next === "all") {
      setMap(new Map([[ALL_TARGET, new Set<Permission>()]]));
    } else {
      setMap(new Map());
    }
  }

  function updateTarget(key: string, perms: Set<Permission>) {
    setMap((prev) => {
      const next = new Map(prev ?? []);
      next.set(key, perms);
      return next;
    });
  }

  function toggleServer(name: string, included: boolean) {
    setMap((prev) => {
      const next = new Map(prev ?? []);
      if (included) next.set(name, new Set());
      else next.delete(name);
      return next;
    });
  }

  return (
    <Modal title={t("members.permissionsTitle", { name: username })} onClose={onClose} wide>
      <div className="flex flex-col gap-4">
        {showNoAccessHint && (
          <div className="border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary">
            {t("members.noAccessYet")}
          </div>
        )}
        {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("members.permissionsLoadFailed"))}</div>}

        <div className="flex gap-2">
          <Button type="button" variant={mode === "all" ? "primary" : "secondary"} onClick={() => switchMode("all")}>
            {t("members.allServers")}
          </Button>
          <Button type="button" variant={mode === "chosen" ? "primary" : "secondary"} onClick={() => switchMode("chosen")}>
            {t("members.chosenServers")}
          </Button>
        </div>

        {mode === "all" ? (
          <PermissionFields perms={map.get(ALL_TARGET) ?? new Set()} onChange={(next) => updateTarget(ALL_TARGET, next)} />
        ) : (
          <div className="flex flex-col gap-3">
            {servers.length === 0 && <div className="font-prose text-sm text-text-tertiary">{t("orgs.noServers")}</div>}
            {servers.map((s) => {
              const included = map.has(s.metadata.name);
              return (
                <div key={s.metadata.name} className="flex flex-col gap-2 border border-border p-3">
                  <label className="flex items-center gap-2 font-sans text-sm font-semibold text-text-primary">
                    <input
                      type="checkbox"
                      checked={included}
                      onChange={(e) => toggleServer(s.metadata.name, e.target.checked)}
                    />
                    {serverTitle(s)}
                  </label>
                  {included && (
                    <PermissionFields
                      perms={map.get(s.metadata.name) ?? new Set()}
                      onChange={(next) => updateTarget(s.metadata.name, next)}
                    />
                  )}
                </div>
              );
            })}
          </div>
        )}

        <div className="flex items-center gap-3">
          <Button type="button" disabled={save.isPending} onClick={() => save.mutate(mapToGrants(map))}>
            {save.isPending ? t("common.saving") : t("common.save")}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t("common.close")}
          </Button>
          {message && <span className={`font-sans text-sm ${message.ok ? "text-primary-text" : "text-status-failed"}`}>{message.text}</span>}
        </div>
      </div>
    </Modal>
  );
}
