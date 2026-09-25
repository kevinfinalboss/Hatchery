import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AuthLayout, FormError, FormNote } from "../components/layout/AuthLayout";
import { Button } from "../components/ui/Button";
import { Field, Input } from "../components/ui/Input";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { errorMessage } from "../lib/errors";
import { useT, type TKey } from "../lib/i18n";
import { useOrg } from "../lib/org";
import type { LoginResponse, OrgRole } from "../lib/types";
import { useFragmentToken } from "../lib/useFragmentToken";
import { NewPasswordFields } from "./ResetPasswordPage";

const roleKey = { owner: "members.roleOwner", admin: "members.roleAdmin", member: "members.roleMember" } as const satisfies Record<OrgRole, TKey>;

const PENDING_KEY = "hatchery_pending_invite";

function readToken(fromFragment: string): string {
  if (fromFragment) {
    try {
      sessionStorage.setItem(PENDING_KEY, fromFragment);
    } catch {
    }
    return fromFragment;
  }
  try {
    return sessionStorage.getItem(PENDING_KEY) ?? "";
  } catch {
    return "";
  }
}

function clearToken() {
  try {
    sessionStorage.removeItem(PENDING_KEY);
  } catch {
  }
}

export function InvitePage() {
  const t = useT();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user, loading, logout, startSession } = useAuth();
  const { setCurrent } = useOrg();
  const fragment = useFragmentToken();
  const [effective] = useState(() => readToken(fragment));

  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const preview = useQuery({
    queryKey: ["invitation", effective],
    queryFn: () => api.lookupInvitation(effective),
    enabled: !!effective,
  });

  async function finish(orgSlug: string) {
    clearToken();
    await queryClient.invalidateQueries({ queryKey: ["orgs"] });
    setCurrent(orgSlug);
    navigate("/", { replace: true });
  }

  async function acceptNew(e: FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      setError(t("auth.passwordsDiffer"));
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const resp = (await api.acceptInvitation(effective, { username: username.trim(), password, displayName: displayName.trim() })) as LoginResponse;
      startSession(resp);
      await finish(preview.data!.orgSlug);
    } catch (err) {
      setError(errorMessage(err, t("auth.invalidLink")));
    } finally {
      setSubmitting(false);
    }
  }

  async function acceptExisting() {
    setError(null);
    setSubmitting(true);
    try {
      await api.acceptInvitation(effective);
      await finish(preview.data!.orgSlug);
    } catch (err) {
      setError(errorMessage(err, t("auth.invalidLink")));
    } finally {
      setSubmitting(false);
    }
  }

  if (!effective || preview.isError) {
    return (
      <AuthLayout title={t("auth.inviteTitle")}>
        <FormError message={t("auth.invalidLink")} />
        <Link to="/login" className="text-center font-sans text-sm text-text-secondary hover:text-primary-text">
          {t("auth.backToLogin")}
        </Link>
      </AuthLayout>
    );
  }
  if (loading || preview.isLoading || !preview.data) {
    return <AuthLayout title={t("auth.inviteTitle")}><FormNote>{t("common.loading")}</FormNote></AuthLayout>;
  }

  const inv = preview.data;
  const body = t("auth.inviteBody", { invitedBy: inv.invitedBy || "—", email: inv.email, org: inv.orgName, role: t(roleKey[inv.role]) });

  return (
    <AuthLayout title={t("auth.inviteTitle")}>
      <FormNote>{body}</FormNote>

      {inv.accountExists ? (
        !user ? (
          <>
            <FormNote>{t("auth.signInToAccept")}</FormNote>
            <Button onClick={() => navigate("/login?next=/invite")} className="w-full justify-center">
              {t("auth.signIn")}
            </Button>
          </>
        ) : user.email !== inv.email ? (
          <>
            <FormError message={t("auth.wrongAccount", { name: user.username, email: inv.email })} />
            <Button variant="ghost" onClick={() => void logout()} className="w-full justify-center">
              {t("auth.signOut")}
            </Button>
          </>
        ) : (
          <Button onClick={() => void acceptExisting()} disabled={submitting} className="w-full justify-center">
            {submitting ? t("auth.accepting") : t("auth.acceptAs", { name: user.displayName || user.username })}
          </Button>
        )
      ) : (
        <form onSubmit={(e) => void acceptNew(e)} className="flex flex-col gap-4">
          <FormNote>{t("auth.inviteCreateAccount")}</FormNote>
          <Field label={t("auth.displayName")} htmlFor="display-name">
            <Input id="display-name" autoComplete="name" maxLength={64} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
          </Field>
          <Field label={t("auth.username")} htmlFor="username">
            <Input id="username" autoComplete="username" pattern="[A-Za-z0-9._\-]{2,32}" value={username} onChange={(e) => setUsername(e.target.value)} required />
          </Field>
          <div className="-mt-2 font-sans text-xs text-text-tertiary">{t("auth.usernameHint")}</div>
          <NewPasswordFields password={password} confirm={confirm} onPassword={setPassword} onConfirm={setConfirm} />
          <Button type="submit" disabled={submitting} className="w-full justify-center">
            {submitting ? t("auth.accepting") : t("auth.acceptAndCreate")}
          </Button>
        </form>
      )}
      <FormError message={error} />
    </AuthLayout>
  );
}
