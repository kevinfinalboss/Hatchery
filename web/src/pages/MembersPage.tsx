import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useT, type TKey } from "../lib/i18n";
import { useAuth } from "../lib/auth";
import { atLeast, useOrg } from "../lib/org";
import { errorMessage } from "../lib/errors";
import type { Member, OrgRole } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { Badge, ListRow, RowActions } from "../components/ui/List";
import { Filtered } from "../components/ui/Filtered";

const roleKey = {
  owner: "members.roleOwner",
  admin: "members.roleAdmin",
  member: "members.roleMember",
} as const satisfies Record<OrgRole, TKey>;

const selectClasses =
  "rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary";

function AddMemberForm({ org, callerRole }: { org: string; callerRole: OrgRole }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<OrgRole>("member");
  const [error, setError] = useState<string | null>(null);

  const add = useMutation({
    mutationFn: () => api.addMember(org, username.trim(), role),
    onSuccess: () => {
      setUsername("");
      setRole("member");
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["members", org] });
    },
    onError: (err) => setError(errorMessage(err, t("members.addFailed"))),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">{t("members.addTitle")}</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          add.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label={t("common.username")} htmlFor="member-username">
          <Input id="member-username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        </Field>
        <Field label={t("members.role")} htmlFor="member-role">
          <select id="member-role" value={role} onChange={(e) => setRole(e.target.value as OrgRole)} className={selectClasses}>
            <option value="member">{t("members.roleMember")}</option>
            <option value="admin">{t("members.roleAdmin")}</option>
            {callerRole === "owner" && <option value="owner">{t("members.roleOwner")}</option>}
          </select>
        </Field>
        <Button type="submit" disabled={add.isPending}>
          {add.isPending ? t("members.adding") : t("members.add")}
        </Button>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

function MemberRow({
  org,
  member,
  callerRole,
  selfId,
  onError,
}: {
  org: string;
  member: Member;
  callerRole: OrgRole;
  selfId: number | undefined;
  onError: (msg: string | null) => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const isSelf = member.userId === selfId;
  // Only an owner deals in owners; an admin manages members and other admins.
  const canManage = atLeast(callerRole, "admin") && (member.role !== "owner" || callerRole === "owner");
  const canLeaveOnly = isSelf && !canManage;

  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["members", org] });

  const changeRole = useMutation({
    mutationFn: (role: OrgRole) => api.setMemberRole(org, member.userId, role),
    onSuccess: () => {
      onError(null);
      invalidate();
    },
    onError: (err) => onError(errorMessage(err, t("members.roleChangeFailed"))),
  });
  const remove = useMutation({
    mutationFn: () => api.removeMember(org, member.userId),
    onSuccess: () => {
      onError(null);
      invalidate();
      // Leaving the org removes it from the user's own list too.
      if (isSelf) void queryClient.invalidateQueries({ queryKey: ["orgs"] });
    },
    onError: (err) => onError(errorMessage(err, t("members.removeFailed"))),
  });

  return (
    <ListRow>
      <div className="min-w-0">
        <span className="font-display text-[15px] font-semibold text-text-primary">{member.username}</span>
        {isSelf && <span className="ml-2 font-sans text-xs text-text-tertiary">{t("members.you")}</span>}
      </div>
      <RowActions>
        {canManage ? (
          <select
            aria-label={t("members.roleOf", { name: member.username })}
            value={member.role}
            disabled={changeRole.isPending}
            onChange={(e) => changeRole.mutate(e.target.value as OrgRole)}
            className={selectClasses}
          >
            <option value="member">{t("members.roleMember")}</option>
            <option value="admin">{t("members.roleAdmin")}</option>
            {(callerRole === "owner" || member.role === "owner") && <option value="owner">{t("members.roleOwner")}</option>}
          </select>
        ) : (
          <Badge>{t(roleKey[member.role])}</Badge>
        )}
        {(canManage || canLeaveOnly) && (
          <Button
            variant="ghost"
            disabled={remove.isPending}
            onClick={() => {
              const q = isSelf ? t("members.leaveConfirm") : t("members.removeConfirm", { name: member.username });
              if (confirm(q)) remove.mutate();
            }}
          >
            {isSelf ? t("common.leave") : t("common.remove")}
          </Button>
        )}
      </RowActions>
    </ListRow>
  );
}

export function MembersPage() {
  const t = useT();
  const { user } = useAuth();
  const { current } = useOrg();
  const [rowError, setRowError] = useState<string | null>(null);
  const org = current?.slug ?? "";

  const { data: members, isLoading, error } = useQuery({
    queryKey: ["members", org],
    queryFn: () => api.listMembers(org),
    enabled: !!org,
  });

  if (!current) return <div className="font-sans text-sm text-text-secondary">{t("common.selectOrg")}</div>;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <div className="font-display text-2xl font-bold text-text-primary">{t("members.title")}</div>
        <div className="mt-0.5 font-sans text-sm text-text-secondary">{current.name}</div>
      </div>

      {atLeast(current.role, "admin") && <AddMemberForm org={org} callerRole={current.role} />}

      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("members.loadFailed"))}</div>}
      {rowError && <div className="font-sans text-sm text-status-failed">{rowError}</div>}

      <Filtered items={members ?? []} text={(m) => m.username} placeholder={t("members.filterPlaceholder")}>
        {(items) => (
          <Card className="divide-y divide-border">
            {items.map((m) => (
              <MemberRow key={m.userId} org={org} member={m} callerRole={current.role} selfId={user?.id} onError={setRowError} />
            ))}
          </Card>
        )}
      </Filtered>
    </div>
  );
}
