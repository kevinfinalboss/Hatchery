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
  const { user, login, loginTwoFactor } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const t = useT();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [ticket, setTicket] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [useRecovery, setUseRecovery] = useState(false);
  const { data: features } = useQuery({ queryKey: ["auth-features"], queryFn: api.features });

  const rawNext = new URLSearchParams(location.search).get("next");
  const next = rawNext && /^\/(?![/\\])/.test(rawNext) ? rawNext : "/";

  if (user) return <Navigate to={next} replace />;

  function showError(err: unknown) {
    if (err instanceof ApiError && err.status === 429) {
      setError(
        err.retryAfter
          ? t("login.tooManyAttemptsRetry", { minutes: Math.ceil(err.retryAfter / 60) })
          : t("login.tooManyAttempts"),
      );
    } else {
      setError(err instanceof ApiError ? err.message : t("login.connectFailed"));
    }
  }

  async function handleCode(e: FormEvent) {
    e.preventDefault();
    if (!ticket) return;
    setError(null);
    setSubmitting(true);
    try {
      const resp = await loginTwoFactor(ticket, code);
      navigate(resp.recoveryCodesLeft !== undefined ? "/account" : next, { replace: true });
    } catch (err) {
      if (err instanceof ApiError && err.code === "ticket_expired") {
        setTicket(null);
        setCode("");
        setPassword("");
        setError(t("twoFactor.loginExpired"));
      } else if (err instanceof ApiError && err.status === 401) {
        setError(t("twoFactor.wrongCode"));
      } else {
        showError(err);
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const challenge = await login(username, password);
      if (challenge) {
        setTicket(challenge.ticket);
        setCode("");
        setUseRecovery(false);
        return;
      }
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

  if (ticket) {
    return (
      <AuthLayout>
        <form onSubmit={(e) => void handleCode(e)} className="flex flex-col gap-5">
          <div className="font-prose text-sm text-text-secondary">
            {useRecovery ? t("twoFactor.loginRecoveryHelp") : t("twoFactor.loginHelp")}
          </div>
          <Field label={useRecovery ? t("twoFactor.recoveryCode") : t("twoFactor.code")} htmlFor="code">
            <Input
              id="code"
              key={useRecovery ? "recovery" : "totp"}
              autoFocus
              autoComplete="one-time-code"
              inputMode={useRecovery ? "text" : "numeric"}
              maxLength={useRecovery ? 11 : 6}
              placeholder={useRecovery ? "xxxxx-xxxxx" : "123456"}
              value={code}
              onChange={(e) => setCode(useRecovery ? e.target.value : e.target.value.replace(/\D/g, ""))}
              required
            />
          </Field>

          {error && <div className="font-sans text-sm text-status-failed">{error}</div>}

          <Button type="submit" disabled={submitting} className="w-full justify-center">
            {submitting ? t("login.submitting") : t("twoFactor.verify")}
          </Button>
          <button
            type="button"
            onClick={() => {
              setUseRecovery(!useRecovery);
              setCode("");
              setError(null);
            }}
            className="text-center font-sans text-sm text-text-secondary hover:text-primary-text"
          >
            {useRecovery ? t("twoFactor.useApp") : t("twoFactor.useRecovery")}
          </button>
          <button
            type="button"
            onClick={() => {
              setTicket(null);
              setPassword("");
              setError(null);
            }}
            className="text-center font-sans text-sm text-text-tertiary hover:text-text-secondary"
          >
            {t("twoFactor.backToPassword")}
          </button>
        </form>
      </AuthLayout>
    );
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
