import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { useT } from "../lib/i18n";
import { Avatar } from "../components/ui/Avatar";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { ListRow, RowActions } from "../components/ui/List";
import { Filtered } from "../components/ui/Filtered";
import { errorMessage } from "../lib/errors";

const INITIAL_ADMIN = "admin";

export function UsersPage() {
  const t = useT();
  const queryClient = useQueryClient();
  const { user: self } = useAuth();
  const { data: users, isLoading } = useQuery({ queryKey: ["users"], queryFn: api.listUsers });

  const [rowError, setRowError] = useState<string | null>(null);
  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["users"] });

  const deleteUser = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => {
      setRowError(null);
      invalidate();
    },
    onError: (err) => setRowError(errorMessage(err, t("users.deleteFailed"))),
  });
  const setAdmin = useMutation({
    mutationFn: (v: { id: number; isAdmin: boolean }) => api.setUserAdmin(v.id, v.isAdmin),
    onSuccess: () => {
      setRowError(null);
      invalidate();
    },
    onError: (err) => setRowError(errorMessage(err, t("users.adminFailed"))),
  });

  return (
    <div className="flex flex-col gap-6">
      <div>
        <div className="font-display text-2xl font-bold text-text-primary">{t("users.title")}</div>
        <div className="mt-0.5 font-sans text-sm text-text-secondary">{t("users.inviteHint")}</div>
      </div>

      {rowError && <div className="font-sans text-sm text-status-failed">{rowError}</div>}

      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}

      <Filtered items={users ?? []} text={(u) => `${u.username} ${u.displayName} ${u.email}`} placeholder={t("users.filterPlaceholder")}>
        {(items) => (
          <Card className="divide-y divide-border">
            {items.map((user) => (
              <ListRow key={user.id}>
                <div className="flex min-w-0 items-center gap-3">
                  <Avatar user={user} size="md" />
                  <div className="min-w-0">
                    <div>
                      <span className="font-display text-[15px] font-semibold text-text-primary">{user.displayName || user.username}</span>
                      {user.displayName && <span className="ml-2 font-mono text-xs text-text-tertiary">{user.username}</span>}
                    </div>
                    <div className="font-sans text-xs text-text-tertiary">{user.email}</div>
                  </div>
                </div>
                <RowActions>
                  <label className="flex items-center gap-2 font-sans text-sm text-text-secondary">
                    <input
                      type="checkbox"
                      checked={user.isAdmin}
                      disabled={user.id === self?.id || setAdmin.isPending}
                      onChange={(e) => setAdmin.mutate({ id: user.id, isAdmin: e.target.checked })}
                    />
                    {t("users.platformAdmin")}
                  </label>
                  {user.username !== INITIAL_ADMIN && user.id !== self?.id && (
                    <Button
                      variant="ghost"
                      onClick={() => {
                        if (confirm(t("users.deleteConfirm", { name: user.username }))) deleteUser.mutate(user.id);
                      }}
                    >
                      {t("common.delete")}
                    </Button>
                  )}
                </RowActions>
              </ListRow>
            ))}
          </Card>
        )}
      </Filtered>
    </div>
  );
}
