import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import { Button } from "../ui/Button";

export function OrgDanger({ slug, name }: { slug: string; name: string }) {
  const t = useT();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);

  const remove = useMutation({
    mutationFn: () => api.deleteOrg(slug),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["orgs"] });
      navigate("/orgs");
    },
    onError: (err) => setError(errorMessage(err, t("orgs.deleteFailed"))),
  });

  return (
    <div>
      <div className="mb-2 font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("orgs.dangerTitle")}</div>
      <Button
        variant="danger"
        disabled={remove.isPending}
        onClick={() => {
          setError(null);
          if (confirm(t("orgs.deleteConfirm", { name }))) remove.mutate();
        }}
      >
        {t("orgs.deleteOrg")}
      </Button>
      {error && <div className="mt-2 font-sans text-sm text-status-failed">{error}</div>}
    </div>
  );
}
