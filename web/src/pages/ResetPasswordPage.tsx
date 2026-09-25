import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { AuthLayout, FormError, FormNote } from "../components/layout/AuthLayout";
import { Button } from "../components/ui/Button";
import { Field, Input } from "../components/ui/Input";
import { api } from "../lib/api";
import { errorMessage } from "../lib/errors";
import { useT } from "../lib/i18n";
import { useFragmentToken } from "../lib/useFragmentToken";

export function NewPasswordFields({
  password,
  confirm,
  onPassword,
  onConfirm,
}: {
  password: string;
  confirm: string;
  onPassword: (v: string) => void;
  onConfirm: (v: string) => void;
}) {
  const t = useT();
  return (
    <>
      <Field label={t("auth.newPassword")} htmlFor="new-password">
        <Input id="new-password" type="password" autoComplete="new-password" minLength={8} maxLength={72} value={password} onChange={(e) => onPassword(e.target.value)} required />
      </Field>
      <Field label={t("auth.confirmPassword")} htmlFor="confirm-password">
        <Input id="confirm-password" type="password" autoComplete="new-password" value={confirm} onChange={(e) => onConfirm(e.target.value)} required />
      </Field>
      <div className="font-sans text-xs text-text-tertiary">{t("auth.passwordHint")}</div>
    </>
  );
}

export function ResetPasswordPage() {
  const t = useT();
  const token = useFragmentToken();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(token ? null : t("auth.invalidLink"));
  const [submitting, setSubmitting] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      setError(t("auth.passwordsDiffer"));
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      await api.resetPassword(token, password);
      setDone(true);
    } catch (err) {
      setError(errorMessage(err, t("auth.invalidLink")));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout title={t("auth.resetTitle")}>
      {done ? (
        <FormNote>{t("auth.resetDone")}</FormNote>
      ) : token ? (
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4">
          <NewPasswordFields password={password} confirm={confirm} onPassword={setPassword} onConfirm={setConfirm} />
          <FormError message={error} />
          <Button type="submit" disabled={submitting} className="w-full justify-center">
            {submitting ? t("common.saving") : t("auth.savePassword")}
          </Button>
        </form>
      ) : (
        <FormError message={error} />
      )}
      <Link to="/login" className="text-center font-sans text-sm text-text-secondary hover:text-primary-text">
        {t("auth.backToLogin")}
      </Link>
    </AuthLayout>
  );
}
