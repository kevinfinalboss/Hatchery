import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useI18n, useT, type TKey } from "../lib/i18n";
import { useAuth } from "../lib/auth";
import { atLeast, useOrg } from "../lib/org";
import { errorMessage } from "../lib/errors";
import type { Member, OrgRole } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { Badge, ListRow, RowActions } from "../components/ui/List";
import { Filtered } from "../components/ui/Filtered";
import { MemberPermissionsEditor } from "../components/orgs/MemberPermissionsEditor";
import { Avatar } from "../components/ui/Avatar";

const roleKey = {
  owner: "members.roleOwner",
  admin: "members.roleAdmin",
  member: "members.roleMember",
} as const satisfies Record<OrgRole, TKey>;

const selectClasses =
  "rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary";

function CopyableLink({ url }: { url: string }) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <code className="max-w-full truncate rounded border border-border bg-canvas px-2 py-1 font-mono text-xs text-text-secondary">{url}</code>
      <Button
        variant="ghost"
        type="button"
        onClick={() => {
          void navigator.clipboard.writeText(url).then(() => setCopied(true));
        }}
      >
        {copied ? t("members.copied") : t("members.copyLink")}
      </Button>
    </div>
  );
}

function InviteForm({ org, callerRole }: { org: string; callerRole: OrgRole }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<OrgRole>("member");
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ email: string; url?: string } | null>(null);

  const invite = useMutation({
    mutationFn: () => api.invite(org, email.trim(), role),
    onSuccess: (resp) => {
      setResult({ email: resp.invitation.email, url: resp.inviteUrl });
      setEmail("");
      setRole("member");
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["invitations", org] });
    },
    onError: (err) => {
      setResult(null);
      setError(errorMessage(err, t("members.inviteFailed")));
    },
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">{t("members.inviteTitle")}</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          invite.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label={t("members.inviteEmail")} htmlFor="invite-email">
          <Input id="invite-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </Field>
        <Field label={t("members.role")} htmlFor="invite-role">
          <select id="invite-role" value={role} onChange={(e) => setRole(e.target.value as OrgRole)} className={selectClasses}>
            <option value="member">{t("members.roleMember")}</option>
            <option value="admin">{t("members.roleAdmin")}</option>
            {callerRole === "owner" && <option value="owner">{t("members.roleOwner")}</option>}
          </select>
        </Field>
        <Button type="submit" disabled={invite.isPending}>
          {invite.isPending ? t("members.inviting") : t("members.invite")}
        </Button>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
      {result && !result.url && <div className="font-sans text-sm text-status-running">{t("members.inviteSent", { email: result.email })}</div>}
      {result?.url && (
        <div className="flex flex-col gap-2">
          <div className="font-sans text-sm text-text-secondary">{t("members.inviteLinkHelp")}</div>
          <CopyableLink url={result.url} />
        </div>
      )}
    </Card>
  );
}

function PendingInvitations({ org, callerRole }: { org: string; callerRole: OrgRole }) {
  const t = useT();
  const { formatDateTime } = useI18n();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const [link, setLink] = useState<string | null>(null);
  const { data } = useQuery({ queryKey: ["invitations", org], queryFn: () => api.listInvitations(org) });
  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["invitations", org] });

  const resend = useMutation({
    mutationFn: (id: number) => api.resendInvitation(org, id),
    onSuccess: (resp) => {
      setError(null);
      setLink(resp.inviteUrl ?? null);
      invalidate();
    },
    onError: (err) => setError(errorMessage(err, t("members.inviteActionFailed"))),
  });
  const revoke = useMutation({
    mutationFn: (id: number) => api.revokeInvitation(org, id),
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) => setError(errorMessage(err, t("members.inviteActionFailed"))),
  });

  if (!data || data.length === 0) return null;
  return (
    <div className="flex flex-col gap-2">
      <div className="font-display text-base font-semibold text-text-primary">{t("members.pendingTitle")}</div>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
      {link && <CopyableLink url={link} />}
      <Card className="divide-y divide-border">
        {data.map((inv) => (
          <ListRow key={inv.id}>
            <div className="min-w-0">
              <span className="font-sans text-sm text-text-primary">{inv.email}</span>
              <span className="ml-2 font-sans text-xs text-text-tertiary">
                {inv.expired ? t("members.expired") : t("members.expires", { date: formatDateTime(inv.expiresAt) })}
                {inv.invitedBy && ` · ${t("members.invitedBy", { name: inv.invitedBy })}`}
              </span>
            </div>
            <RowActions>
              <Badge>{t(roleKey[inv.role])}</Badge>
              {(inv.role !== "owner" || callerRole === "owner") && (
                <>
                  <Button variant="ghost" disabled={resend.isPending} onClick={() => resend.mutate(inv.id)}>
                    {t("members.resend")}
                  </Button>
                  <Button
                    variant="ghost"
                    disabled={revoke.isPending}
                    onClick={() => {
                      if (confirm(t("members.revokeConfirm", { email: inv.email }))) revoke.mutate(inv.id);
                    }}
                  >
                    {t("members.revoke")}
                  </Button>
                </>
              )}
            </RowActions>
          </ListRow>
        ))}
      </Card>
    </div>
  );
}

function MemberRow({
  org,
  member,
  callerRole,
  selfId,
  onError,
  onOpenPermissions,
}: {
  org: string;
  member: Member;
  callerRole: OrgRole;
  selfId: number | undefined;
  onError: (msg: string | null) => void;
  onOpenPermissions: (userId: number, username: string, showHint: boolean) => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const isSelf = member.userId === selfId;
  // Only an owner deals in owners; an admin manages members and other admins.
  const canManage = atLeast(callerRole, "admin") && (member.role !== "owner" || callerRole === "owner");
  const canLeaveOnly = isSelf && !canManage;
  const canEditPermissions = atLeast(callerRole, "admin");

  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["members", org] });

  const changeRole = useMutation({
    mutationFn: (role: OrgRole) => api.setMemberRole(org, member.userId, role),
    onSuccess: (result) => {
      onError(null);
      invalidate();
      if (!result.hasServerAccess) onOpenPermissions(member.userId, member.username, true);
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
      <div className="flex min-w-0 items-center gap-3">
        <Avatar user={member} size="md" />
        <div className="min-w-0">
          <div>
            <span className="font-display text-[15px] font-semibold text-text-primary">{member.displayName || member.username}</span>
            {member.displayName && <span className="ml-2 font-mono text-xs text-text-tertiary">{member.username}</span>}
            {isSelf && <span className="ml-2 font-sans text-xs text-text-tertiary">{t("members.you")}</span>}
          </div>
          <div className="flex flex-wrap gap-x-3 font-sans text-xs text-text-tertiary">
            {member.email && <span>{member.email}</span>}
            {member.discord && <span>@{member.discord}</span>}
            {member.minecraftUsername && <span>{t("members.minecraft", { name: member.minecraftUsername })}</span>}
            {member.steamId && (
              <a href={`https://steamcommunity.com/profiles/${member.steamId}`} target="_blank" rel="noreferrer" className="hover:text-primary-text">
                {t("members.steam")}
              </a>
            )}
          </div>
        </div>
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
        {canEditPermissions && (
          <Button variant="ghost" onClick={() => onOpenPermissions(member.userId, member.username, false)}>
            {t("members.permissions")}
          </Button>
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
  const [permEditor, setPermEditor] = useState<{ userId: number; username: string; showHint: boolean } | null>(null);
  const org = current?.slug ?? "";

  const { data: members, isLoading, error } = useQuery({
    queryKey: ["members", org],
    queryFn: () => api.listMembers(org),
    enabled: !!org,
  });

  function openPermissions(userId: number, username: string, showHint: boolean) {
    setPermEditor({ userId, username, showHint });
  }

  if (!current) return <div className="font-sans text-sm text-text-secondary">{t("common.selectOrg")}</div>;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <div className="font-display text-2xl font-bold text-text-primary">{t("members.title")}</div>
        <div className="mt-0.5 font-sans text-sm text-text-secondary">{current.name}</div>
      </div>

      {atLeast(current.role, "admin") && (
        <>
          <InviteForm org={org} callerRole={current.role} />
          <PendingInvitations org={org} callerRole={current.role} />
        </>
      )}

      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("members.loadFailed"))}</div>}
      {rowError && <div className="font-sans text-sm text-status-failed">{rowError}</div>}

      <Filtered items={members ?? []} text={(m) => `${m.username} ${m.displayName} ${m.email ?? ""}`} placeholder={t("members.filterPlaceholder")}>
        {(items) => (
          <Card className="divide-y divide-border">
            {items.map((m) => (
              <MemberRow
                key={m.userId}
                org={org}
                member={m}
                callerRole={current.role}
                selfId={user?.id}
                onError={setRowError}
                onOpenPermissions={openPermissions}
              />
            ))}
          </Card>
        )}
      </Filtered>

      {permEditor && (
        <MemberPermissionsEditor
          org={org}
          userId={permEditor.userId}
          username={permEditor.username}
          showNoAccessHint={permEditor.showHint}
          onClose={() => setPermEditor(null)}
        />
      )}
    </div>
  );
}
