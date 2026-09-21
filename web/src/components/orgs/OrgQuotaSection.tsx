import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import type { OrgQuota } from "../../lib/types";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { DEFAULT_QUOTA } from "../../lib/orgQuota";
import { QuotaFields } from "./QuotaFields";

export function OrgQuotaSection({ slug, quota }: { slug: string; quota: OrgQuota | undefined }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<OrgQuota | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const value = draft ?? quota ?? DEFAULT_QUOTA;

  const save = useMutation({
    mutationFn: () => api.updateOrgQuota(slug, value),
    onSuccess: () => {
      setDraft(null);
      setMessage({ ok: true, text: t("orgs.quotaSaved") });
      void queryClient.invalidateQueries({ queryKey: ["org", slug] });
      void queryClient.invalidateQueries({ queryKey: ["quota", slug] });
    },
    onError: (err) => setMessage({ ok: false, text: errorMessage(err, t("orgs.quotaUpdateFailed")) }),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("orgs.quota")}</div>
      <form
        className="flex flex-wrap items-end gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          setMessage(null);
          save.mutate();
        }}
      >
        <QuotaFields quota={value} onChange={setDraft} />
        <Button type="submit" disabled={!draft || save.isPending || !quota}>
          {save.isPending ? t("common.saving") : t("orgs.saveQuota")}
        </Button>
      </form>
      {message && <div className={`font-sans text-sm ${message.ok ? "text-primary-text" : "text-status-failed"}`}>{message.text}</div>}
    </Card>
  );
}
