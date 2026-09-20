import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { errorMessage } from "../lib/errors";

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

  const [deleteError, setDeleteError] = useState<string | null>(null);
  const deleteUser = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => {
      setDeleteError(null);
      void queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => setDeleteError(errorMessage(err, "Falha ao excluir usuário")),
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="font-display text-2xl font-bold text-text-primary">Usuários</div>

      <CreateUserForm />

      {deleteError && <div className="font-sans text-sm text-status-failed">{deleteError}</div>}

      {isLoading && <div className="font-sans text-sm text-text-secondary">Carregando…</div>}

      <div className="flex flex-col gap-3">
        {(users ?? []).map((user) => (
          <Card key={user.id} className="flex items-center justify-between p-5">
            <div>
              <span className="font-display text-[15px] font-semibold text-text-primary">{user.username}</span>
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
          </Card>
        ))}
      </div>
    </div>
  );
}
