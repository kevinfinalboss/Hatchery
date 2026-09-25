import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { errorMessage } from "../../lib/errors";
import { useT } from "../../lib/i18n";
import { modpacksApi } from "../../lib/modpacks";
import { Button } from "../ui/Button";

export function ModpackCard({ org, name, canUpdate, onBackups }: { org: string; name: string; canUpdate: boolean; onBackups: () => void }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const { data } = useQuery({ queryKey: ["modpack", org, name], queryFn: () => modpacksApi.status(org, name), retry: false });
  const update = useMutation({
    mutationFn: (versionId: string) => modpacksApi.update(org, name, versionId),
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["modpack", org, name] });
      void queryClient.invalidateQueries({ queryKey: ["gameserver", org, name] });
    },
    onError: (err) => setError(errorMessage(err, t("modpacks.updateFailed"))),
  });

  if (!data) return null;
  const current = data.version;
  const latest = data.latest;

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border border-border bg-surface px-4 py-2.5 font-sans text-sm">
      <span className="text-text-secondary">
        {t("modpacks.running", {
          version: current ? current.versionNumber || current.name : data.projectId,
          game: current?.gameVersion ?? "?",
          loaders: (current?.loaders ?? []).join(", "),
        })}
      </span>
      {data.updateAvailable && latest && canUpdate && (
        <span className="flex items-center gap-2">
          <Button variant="ghost" onClick={onBackups}>
            {t("modpacks.backupFirst")}
          </Button>
          <Button
            variant="secondary"
            disabled={update.isPending}
            onClick={() => {
              if (confirm(t("modpacks.updateConfirm", { version: latest.versionNumber || latest.name }))) update.mutate(latest.id);
            }}
          >
            {t("modpacks.updateTo", { version: latest.versionNumber || latest.name })}
          </Button>
        </span>
      )}
      {error && <span className="w-full text-status-failed">{error}</span>}
    </div>
  );
}
