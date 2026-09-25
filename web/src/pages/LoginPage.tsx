import { useState, type FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AuthLayout } from "../components/layout/AuthLayout";
import { Button } from "../components/ui/Button";
import { Field, Input } from "../components/ui/Input";
import { useT } from "../lib/i18n";
import { useAuth } from "../lib/auth";
import { api, ApiError } from "../lib/api";

export function LoginPage() {
  const { user, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const t = useT();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { data: features } = useQuery({ queryKey: ["auth-features"], queryFn: api.features });

  const rawNext = new URLSearchParams(location.search).get("next");
  const next = rawNext && /^\/(?![/\\])/.test(rawNext) ? rawNext : "/";

  if (user) return <Navigate to={next} replace />;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(username, password);
      navigate(next, { replace: true });
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
    <AuthLayout>
      <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-5">
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

        {features?.passwordReset && (
          <Link to="/forgot-password" className="text-center font-sans text-sm text-text-secondary hover:text-primary-text">
            {t("login.forgot")}
          </Link>
        )}
      </form>
    </AuthLayout>
  );
}
