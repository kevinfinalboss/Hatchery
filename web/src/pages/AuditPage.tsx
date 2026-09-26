import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";
import { useAuth } from "../lib/auth";
import { useOrg } from "../lib/org";
import { errorMessage } from "../lib/errors";
import type { AuditEvent } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";

function actionLabel(t: (key: TKey) => string, action: string): string {
  const key = `audit.actions.${action.replace(/[.-]/g, "_")}` as TKey;
  const label = t(key);
  return label === key ? action : label;
}

const outcomeClasses: Record<AuditEvent["outcome"], string> = {
  success: "text-status-running",
  denied: "text-status-failed",
  failed: "text-status-failed",
};

function EventRow({ e }: { e: AuditEvent }) {
  const { t, formatDateTime } = useI18n();
  return (
    <tr className="border-t border-border">
      <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-text-tertiary">
        {formatDateTime(e.createdAt)}
      </td>
      <td className="px-4 py-2.5 font-sans text-sm text-text-primary">{e.actorUsername}</td>
      <td className="px-4 py-2.5 font-sans text-sm text-text-primary">{actionLabel(t, e.action)}</td>
      <td className="px-4 py-2.5 font-mono text-xs text-text-secondary">
        {e.targetName ? `${e.targetType ?? ""} ${e.targetName}`.trim() : "—"}
      </td>
      <td className={`px-4 py-2.5 font-sans text-xs font-semibold ${outcomeClasses[e.outcome]}`}>{e.outcome}</td>
      <td className="px-4 py-2.5 font-mono text-xs text-text-tertiary">{e.ip ?? ""}</td>
    </tr>
  );
}

export function AuditPage() {
  const { t } = useI18n();
  const { user } = useAuth();
  const { current } = useOrg();
  const [scope, setScope] = useState<"org" | "platform">("org");
  const org = current?.slug ?? "";
  const platform = scope === "platform" && !!user?.isAdmin;

  const { data, isLoading, error, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["audit", platform ? "platform" : org],
    queryFn: ({ pageParam }) => (platform ? api.listPlatformAudit(pageParam) : api.listOrgAudit(org, pageParam)),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.nextBefore ?? undefined,
    enabled: platform || !!org,
  });

  const events = data?.pages.flatMap((p) => p.events) ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="font-display text-2xl font-bold text-text-primary">{t("audit.title")}</div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            {platform ? t("audit.platformSubtitle") : t("audit.orgSubtitle", { org: current?.name ?? "" })}
          </div>
          {data?.pages[0] && (
            <div className="mt-0.5 font-sans text-xs text-text-tertiary">
              {data.pages[0].retentionDays > 0
                ? t("audit.retention", { days: data.pages[0].retentionDays })
                : t("audit.retentionForever")}
            </div>
          )}
        </div>
        {user?.isAdmin && (
          <select
            aria-label={t("audit.scopeAria")}
            value={scope}
            onChange={(e) => setScope(e.target.value as "org" | "platform")}
            className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          >
            <option value="org">{t("audit.scopeOrg")}</option>
            <option value="platform">{t("audit.scopePlatform")}</option>
          </select>
        )}
      </div>

      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("audit.loadFailed"))}</div>}
      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}

      <Card className="overflow-x-auto">
        <table className="w-full text-left">
          <thead>
            <tr className="font-sans text-xs uppercase tracking-wide text-text-tertiary">
              <th className="px-4 py-2.5 font-medium">{t("audit.colWhen")}</th>
              <th className="px-4 py-2.5 font-medium">{t("audit.colWho")}</th>
              <th className="px-4 py-2.5 font-medium">{t("audit.colAction")}</th>
              <th className="px-4 py-2.5 font-medium">{t("audit.colTarget")}</th>
              <th className="px-4 py-2.5 font-medium">{t("audit.colResult")}</th>
              <th className="px-4 py-2.5 font-medium">{t("audit.colIp")}</th>
            </tr>
          </thead>
          <tbody>
            {events.map((e) => (
              <EventRow key={e.id} e={e} />
            ))}
          </tbody>
        </table>
        {!isLoading && events.length === 0 && (
          <div className="px-4 py-6 font-sans text-sm text-text-tertiary">{t("audit.empty")}</div>
        )}
      </Card>

      {hasNextPage && (
        <div>
          <Button variant="secondary" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
            {isFetchingNextPage ? t("common.loading") : t("audit.loadMore")}
          </Button>
        </div>
      )}
    </div>
  );
}
