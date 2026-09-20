import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useT } from "../lib/i18n";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";
import { Badge, ListRow, RowActions } from "../components/ui/List";
import { Filtered } from "../components/ui/Filtered";
import { errorMessage } from "../lib/errors";

function CreateUserForm() {
  const t = useT();
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
    onError: (err) => setError(err instanceof Error ? err.message : t("users.createFailed")),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">{t("users.newTitle")}</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          create.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label={t("common.username")} htmlFor="new-username">
          <Input id="new-username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        </Field>
        <Field label={t("users.password")} htmlFor="new-password">
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
          {t("users.administrator")}
        </label>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? t("common.creating") : t("common.create")}
        </Button>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

export function UsersPage() {
  const t = useT();
  const queryClient = useQueryClient();
  const { data: users, isLoading } = useQuery({ queryKey: ["users"], queryFn: api.listUsers });

  const [deleteError, setDeleteError] = useState<string | null>(null);
  const deleteUser = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => {
      setDeleteError(null);
      void queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => setDeleteError(errorMessage(err, t("users.deleteFailed"))),
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="font-display text-2xl font-bold text-text-primary">{t("users.title")}</div>

      <CreateUserForm />

      {deleteError && <div className="font-sans text-sm text-status-failed">{deleteError}</div>}

      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}

      <Filtered items={users ?? []} text={(u) => u.username} placeholder={t("users.filterPlaceholder")}>
        {(items) => (
          <Card className="divide-y divide-border">
            {items.map((user) => (
              <ListRow key={user.id}>
                <div>
                  <span className="font-display text-[15px] font-semibold text-text-primary">{user.username}</span>
                  {user.isAdmin && <Badge className="ml-2">{t("common.admin")}</Badge>}
                </div>
                <RowActions>
                  <Button
                    variant="ghost"
                    onClick={() => {
                      if (confirm(t("users.deleteConfirm", { name: user.username }))) deleteUser.mutate(user.id);
                    }}
                  >
                    {t("common.delete")}
                  </Button>
                </RowActions>
              </ListRow>
            ))}
          </Card>
        )}
      </Filtered>
    </div>
  );
}
