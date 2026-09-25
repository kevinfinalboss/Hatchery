import { useMemo, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Avatar } from "../components/ui/Avatar";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { errorMessage } from "../lib/errors";
import { useT } from "../lib/i18n";
import type { Profile, ProfileLocale, User } from "../lib/types";
import { NewPasswordFields } from "./ResetPasswordPage";

const selectClasses =
  "rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary";

function supportedTimeZones(): string[] {
  const withZones = Intl as unknown as { supportedValuesOf?: (key: string) => string[] };
  return withZones.supportedValuesOf ? withZones.supportedValuesOf("timeZone") : [];
}

function Status({ ok, error }: { ok: string | null; error: string | null }) {
  if (error) return <div className="font-sans text-sm text-status-failed">{error}</div>;
  if (ok) return <div className="font-sans text-sm text-status-running">{ok}</div>;
  return null;
}

function ProfileSection({ user }: { user: User }) {
  const t = useT();
  const { setUser } = useAuth();
  const [p, setP] = useState<Profile>({
    displayName: user.displayName,
    locale: user.locale,
    timeZone: user.timeZone,
    discord: user.discord,
    minecraftUsername: user.minecraftUsername,
    steamId: user.steamId,
  });
  const [ok, setOk] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const zones = useMemo(() => supportedTimeZones(), []);
  const set = <K extends keyof Profile>(k: K, v: Profile[K]) => setP((cur) => ({ ...cur, [k]: v }));

  async function save(e: FormEvent) {
    e.preventDefault();
    setOk(null);
    setError(null);
    setSaving(true);
    try {
      setUser(await api.updateMe({ ...p, displayName: p.displayName.trim() }));
      setOk(t("account.profileSaved"));
    } catch (err) {
      setError(errorMessage(err, t("account.profileFailed")));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="flex items-center gap-4">
        <Avatar user={{ username: user.username, displayName: p.displayName }} size="lg" />
        <div className="min-w-0">
          <div className="font-display text-base font-semibold text-text-primary">{t("account.profile")}</div>
          <div className="font-sans text-sm text-text-secondary">{t("account.profileHelp")}</div>
        </div>
      </div>
      <form onSubmit={(e) => void save(e)} className="grid gap-4 sm:grid-cols-2">
        <Field label={t("account.displayName")} htmlFor="acc-name">
          <Input id="acc-name" maxLength={64} value={p.displayName} onChange={(e) => set("displayName", e.target.value)} />
        </Field>
        <Field label={t("account.language")} htmlFor="acc-locale">
          <select id="acc-locale" value={p.locale} onChange={(e) => set("locale", e.target.value as ProfileLocale)} className={selectClasses}>
            <option value="">{t("account.languageAuto")}</option>
            <option value="pt-BR">{t("language.pt")}</option>
            <option value="en">{t("language.en")}</option>
          </select>
        </Field>
        <Field label={t("account.timeZone")} htmlFor="acc-tz">
          <select id="acc-tz" value={p.timeZone} onChange={(e) => set("timeZone", e.target.value)} className={selectClasses}>
            <option value="">{t("account.timeZoneAuto")}</option>
            {zones.map((z) => (
              <option key={z} value={z}>
                {z}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t("account.discord")} htmlFor="acc-discord">
          <Input id="acc-discord" pattern="[a-z0-9_.]{2,32}" value={p.discord} onChange={(e) => set("discord", e.target.value.toLowerCase())} />
        </Field>
        <Field label={t("account.minecraft")} htmlFor="acc-mc">
          <Input id="acc-mc" pattern="[A-Za-z0-9_]{3,16}" value={p.minecraftUsername} onChange={(e) => set("minecraftUsername", e.target.value)} />
        </Field>
        <Field label={t("account.steamId")} htmlFor="acc-steam">
          <Input id="acc-steam" inputMode="numeric" pattern="7656119[0-9]{10}" value={p.steamId} onChange={(e) => set("steamId", e.target.value.trim())} />
        </Field>
        <div className="flex items-center gap-4 sm:col-span-2">
          <Button type="submit" disabled={saving}>
            {saving ? t("common.saving") : t("account.saveProfile")}
          </Button>
          <Status ok={ok} error={error} />
        </div>
      </form>
    </Card>
  );
}

function PasswordSection() {
  const t = useT();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [ok, setOk] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function save(e: FormEvent) {
    e.preventDefault();
    setOk(null);
    if (password !== confirm) {
      setError(t("auth.passwordsDiffer"));
      return;
    }
    setError(null);
    setSaving(true);
    try {
      await api.changePassword(current, password);
      setCurrent("");
      setPassword("");
      setConfirm("");
      setOk(t("account.passwordChanged"));
    } catch (err) {
      setError(errorMessage(err, t("account.passwordFailed")));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={(e) => void save(e)} className="flex max-w-sm flex-col gap-4">
      <div className="font-sans text-sm font-semibold text-text-primary">{t("account.changePassword")}</div>
      <Field label={t("account.currentPassword")} htmlFor="acc-current">
        <Input id="acc-current" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
      </Field>
      <NewPasswordFields password={password} confirm={confirm} onPassword={setPassword} onConfirm={setConfirm} />
      <div className="flex items-center gap-4">
        <Button type="submit" disabled={saving}>
          {saving ? t("common.saving") : t("auth.savePassword")}
        </Button>
      </div>
      <Status ok={ok} error={error} />
    </form>
  );
}

function EmailSection({ user, mailEnabled }: { user: User; mailEnabled: boolean }) {
  const t = useT();
  const { setUser } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [ok, setOk] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function save(e: FormEvent) {
    e.preventDefault();
    setOk(null);
    setError(null);
    setSaving(true);
    try {
      const updated = await api.requestEmailChange(email.trim(), password);
      if (updated) {
        setUser(updated);
        setOk(t("account.emailChanged", { email: updated.email }));
      } else {
        setOk(t("account.emailSent", { email: email.trim() }));
      }
      setEmail("");
      setPassword("");
    } catch (err) {
      setError(errorMessage(err, t("account.emailFailed")));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={(e) => void save(e)} className="flex max-w-sm flex-col gap-4">
      <div className="font-sans text-sm font-semibold text-text-primary">{t("account.changeEmail")}</div>
      <div className="font-sans text-sm text-text-secondary">{t("account.currentEmail", { email: user.email })}</div>
      {!mailEnabled && <div className="font-sans text-xs text-text-tertiary">{t("account.emailNoConfirmation")}</div>}
      {(
        <>
          <Field label={t("account.newEmail")} htmlFor="acc-email">
            <Input id="acc-email" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          </Field>
          <Field label={t("account.currentPassword")} htmlFor="acc-email-pw">
            <Input id="acc-email-pw" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </Field>
          <div>
            <Button type="submit" disabled={saving}>
              {saving ? t("common.saving") : mailEnabled ? t("account.sendConfirmation") : t("account.saveEmail")}
            </Button>
          </div>
          <Status ok={ok} error={error} />
        </>
      )}
    </form>
  );
}

export function AccountPage() {
  const t = useT();
  const { user } = useAuth();
  const { data: features } = useQuery({ queryKey: ["auth-features"], queryFn: api.features });
  if (!user) return null;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <div className="font-display text-2xl font-bold text-text-primary">{t("account.title")}</div>
        <div className="mt-0.5 font-sans text-sm text-text-secondary">
          {user.username} · {user.email}
        </div>
      </div>
      <ProfileSection key={user.id} user={user} />
      <Card className="flex flex-col gap-8 p-5">
        <div className="font-display text-base font-semibold text-text-primary">{t("account.security")}</div>
        <div className="grid gap-8 md:grid-cols-2">
          <PasswordSection />
          <EmailSection user={user} mailEnabled={!!features?.passwordReset} />
        </div>
      </Card>
    </div>
  );
}
