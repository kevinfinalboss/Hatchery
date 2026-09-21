import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { formatBytes, parseBytes, parseCpu } from "../../lib/quantity";
import { Card } from "../ui/Card";

function UsageRow({ label, used, limit, text }: { label: string; used: number; limit: number; text: string }) {
  const pct = limit > 0 ? Math.min(100, (used / limit) * 100) : 0;
  const full = limit > 0 && used >= limit;
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-3 font-sans text-sm">
        <span className="text-text-secondary">{label}</span>
        <span className={full ? "text-status-failed" : "text-text-primary"}>{text}</span>
      </div>
      <div className="h-1.5 w-full bg-border">
        <div className={`h-full ${full ? "bg-status-failed" : "bg-primary"}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

export function OrgUsage({ slug }: { slug: string }) {
  const t = useT();
  const { data } = useQuery({ queryKey: ["quota", slug], queryFn: () => api.getQuota(slug), retry: false, refetchInterval: 10000 });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("orgs.usageTitle")}</div>
      {!data && <div className="font-prose text-sm text-text-tertiary">{t("common.loading")}</div>}
      {data && (
        <>
          <UsageRow
            label={t("orgs.usageServers")}
            used={data.used.gameServers}
            limit={data.limit.maxGameServers}
            text={`${data.used.gameServers} / ${data.limit.maxGameServers}`}
          />
          <UsageRow
            label="CPU"
            used={parseCpu(data.used.cpu)}
            limit={parseCpu(data.limit.cpu)}
            text={`${+parseCpu(data.used.cpu).toFixed(2)} / ${data.limit.cpu}`}
          />
          <UsageRow
            label={t("orgs.quotaMemory")}
            used={parseBytes(data.used.memory)}
            limit={parseBytes(data.limit.memory)}
            text={`${formatBytes(parseBytes(data.used.memory))} / ${data.limit.memory}`}
          />
          <UsageRow
            label="Storage"
            used={parseBytes(data.used.storage)}
            limit={parseBytes(data.limit.storage)}
            text={`${formatBytes(parseBytes(data.used.storage))} / ${data.limit.storage}`}
          />
        </>
      )}
    </Card>
  );
}
