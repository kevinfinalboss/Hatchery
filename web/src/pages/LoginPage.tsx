import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { Button } from "../components/ui/Button";
import { Field, Input } from "../components/ui/Input";
import { LanguageSwitcher } from "../components/ui/LanguageSwitcher";
import { Wordmark } from "../components/ui/Wordmark";
import { useT } from "../lib/i18n";
import { useAuth } from "../lib/auth";
import { ApiError } from "../lib/api";

export function LoginPage() {
  const { user, login } = useAuth();
  const navigate = useNavigate();
  const t = useT();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  if (user) return <Navigate to="/" replace />;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(username, password);
      navigate("/", { replace: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 429) {
        setError(
          err.retryAfter
            ? t("login.tooManyAttemptsRetry", { minutes: Math.ceil(err.retryAfter / 60) })
            : t("login.tooManyAttempts"),
        );
      } else {
        setError(err instanceof ApiError ? err.message : t("login.connectFailed"));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex h-screen w-full items-center justify-center bg-canvas px-4">
      <div className="fixed right-4 top-4">
        <LanguageSwitcher />
      </div>
      <form
        onSubmit={(e) => void handleSubmit(e)}
        className="flex w-[360px] max-w-full flex-col gap-5 border border-border bg-surface p-8"
      >
        <div className="text-center text-2xl">
          <Wordmark />
        </div>

        <Field label={t("login.username")} htmlFor="username">
          <Input
            id="username"
            autoFocus
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
          />
        </Field>
        <Field label={t("login.password")} htmlFor="password">
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Field>

        {error && <div className="font-sans text-sm text-status-failed">{error}</div>}

        <Button type="submit" disabled={submitting} className="w-full justify-center">
          {submitting ? t("login.submitting") : t("login.submit")}
        </Button>
      </form>
    </div>
  );
}
