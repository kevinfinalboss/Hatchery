import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import type { User } from "../lib/types";

function UserPermissions({ user }: { user: User }) {
  const queryClient = useQueryClient();
  const [namespace, setNamespace] = useState("default");
  const [name, setName] = useState("");

  const { data: grants } = useQuery({
    queryKey: ["permissions", user.id],
    queryFn: () => api.listUserPermissions(user.id),
  });

  const grant = useMutation({
    mutationFn: () => api.grantPermission(user.id, { namespace, name }),
    onSuccess: () => {
      setName("");
      void queryClient.invalidateQueries({ queryKey: ["permissions", user.id] });
    },
  });

  const revoke = useMutation({
    mutationFn: (ref: { namespace: string; name: string }) => api.revokePermission(user.id, ref),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["permissions", user.id] }),
  });

  if (user.isAdmin) {
    return <div className="font-sans text-xs text-text-tertiary">Admin — acesso a todos os servidores.</div>;
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-1.5">
        {(grants ?? []).length === 0 && (
          <span className="font-sans text-xs text-text-tertiary">Nenhum servidor concedido ainda.</span>
        )}
        {(grants ?? []).map((ref) => (
          <span
            key={`${ref.namespace}/${ref.name}`}
            className="flex items-center gap-1.5 rounded-full bg-surface-hover px-2.5 py-1 font-mono text-[11px] text-text-secondary"
          >
            {ref.namespace}/{ref.name}
            <button
              onClick={() => revoke.mutate(ref)}
              className="text-text-tertiary hover:text-status-failed"
              aria-label={`Revogar acesso a ${ref.name}`}
            >
              ×
            </button>
          </span>
        ))}
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) grant.mutate();
        }}
        className="flex items-end gap-2"
      >
        <Field label="Namespace" htmlFor={`ns-${user.id}`}>
          <Input
            id={`ns-${user.id}`}
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            className="w-28"
          />
        </Field>
        <Field label="Servidor" htmlFor={`name-${user.id}`}>
          <Input
            id={`name-${user.id}`}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="survival-vanilla"
            className="w-44"
          />
        </Field>
        <Button type="submit" variant="secondary" disabled={grant.isPending}>
          Conceder
        </Button>
      </form>
    </div>
  );
}

function CreateUserForm() {
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.createUser({ username, password, isAdmin }),
    onSuccess: () => {
      setUsername("");
      setPassword("");
      setIsAdmin(false);
      void queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => setError(err instanceof Error ? err.message : "Falha ao criar usuário"),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">Novo usuário</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          create.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label="Usuário" htmlFor="new-username">
          <Input id="new-username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        </Field>
        <Field label="Senha" htmlFor="new-password">
          <Input
            id="new-password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Field>
        <label className="flex items-center gap-2 pb-2 font-sans text-sm text-text-secondary">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
          Administrador
        </label>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? "Criando…" : "Criar"}
        </Button>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

export function UsersPage() {
  const queryClient = useQueryClient();
  const { data: users, isLoading } = useQuery({ queryKey: ["users"], queryFn: api.listUsers });

  const deleteUser = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["users"] }),
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="font-display text-2xl font-bold text-text-primary">Usuários</div>

      <CreateUserForm />

      {isLoading && <div className="font-sans text-sm text-text-secondary">Carregando…</div>}

      <div className="flex flex-col gap-3">
        {(users ?? []).map((user) => (
          <Card key={user.id} className="flex flex-col gap-4 p-5">
            <div className="flex items-center justify-between">
              <div>
                <span className="font-display text-[15px] font-semibold text-text-primary">
                  {user.username}
                </span>
                {user.isAdmin && (
                  <span className="ml-2 rounded-full bg-primary/15 px-2 py-0.5 font-sans text-[11px] font-semibold text-primary">
                    Admin
                  </span>
                )}
              </div>
              <Button
                variant="ghost"
                onClick={() => {
                  if (confirm(`Excluir usuário ${user.username}?`)) deleteUser.mutate(user.id);
                }}
              >
                Excluir
              </Button>
            </div>
            <UserPermissions user={user} />
          </Card>
        ))}
      </div>
    </div>
  );
}
