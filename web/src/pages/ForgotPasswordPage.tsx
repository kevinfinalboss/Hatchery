import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { AuthLayout, FormError, FormNote } from "../components/layout/AuthLayout";
import { Button } from "../components/ui/Button";
import { Field, Input } from "../components/ui/Input";
import { api, ApiError } from "../lib/api";
import { errorMessage } from "../lib/errors";
import { useT } from "../lib/i18n";

export function ForgotPasswordPage() {
  const t = useT();
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await api.forgotPassword(email.trim());
      setSent(true);
    } catch (err) {
      if (err instanceof ApiError && err.status === 429) {
        setError(t("auth.tooManyRequests", { minutes: Math.ceil((err.retryAfter ?? 3600) / 60) }));
      } else {
        setError(errorMessage(err, t("login.connectFailed")));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout title={t("auth.forgotTitle")}>
      {sent ? (
        <FormNote>{t("auth.forgotSent")}</FormNote>
      ) : (
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4">
          <FormNote>{t("auth.forgotHelp")}</FormNote>
          <Field label={t("auth.email")} htmlFor="email">
            <Input id="email" type="email" autoComplete="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} required />
          </Field>
          <FormError message={error} />
          <Button type="submit" disabled={submitting} className="w-full justify-center">
            {submitting ? t("auth.sending") : t("auth.sendLink")}
          </Button>
        </form>
      )}
      <Link to="/login" className="text-center font-sans text-sm text-text-secondary hover:text-primary-text">
        {t("auth.backToLogin")}
      </Link>
    </AuthLayout>
  );
}
