import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import logo from "../assets/logo.png";
import { Button } from "../components/ui/Button";
import { Field, Input } from "../components/ui/Input";
import { useAuth } from "../lib/auth";
import { ApiError } from "../lib/api";

export function LoginPage() {
  const { user, login } = useAuth();
  const navigate = useNavigate();
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
      setError(err instanceof ApiError ? err.message : "Falha ao conectar com o Panel API");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex h-screen w-full items-center justify-center bg-canvas px-4">
      <form
        onSubmit={(e) => void handleSubmit(e)}
        className="flex w-[360px] flex-col gap-5 rounded-xl border border-border bg-surface p-8"
      >
        <div className="flex flex-col items-center gap-3">
          <img src={logo} alt="Hatchery" className="h-12 w-12 rounded-xl object-cover" />
          <div className="font-display text-xl font-bold text-text-primary">Hatchery</div>
        </div>

        <Field label="Usuário" htmlFor="username">
          <Input
            id="username"
            autoFocus
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
          />
        </Field>
        <Field label="Senha" htmlFor="password">
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
          {submitting ? "Entrando…" : "Entrar"}
        </Button>
      </form>
    </div>
  );
}
