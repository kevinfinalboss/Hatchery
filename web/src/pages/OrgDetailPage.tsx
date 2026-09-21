import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useT } from "../lib/i18n";
import { errorMessage } from "../lib/errors";
import { Badge } from "../components/ui/List";
import { OrgDanger } from "../components/orgs/OrgDanger";
import { OrgMembers } from "../components/orgs/OrgMembers";
import { OrgQuotaSection } from "../components/orgs/OrgQuotaSection";
import { OrgServers } from "../components/orgs/OrgServers";
import { OrgUsage } from "../components/orgs/OrgUsage";

export function OrgDetailPage() {
  const t = useT();
  const { slug = "" } = useParams();
  const { data: org, error, isLoading } = useQuery({
    queryKey: ["org", slug],
    queryFn: () => api.getOrg(slug),
    refetchInterval: 10000,
  });

  return (
    <div className="flex max-w-4xl flex-col gap-5">
      <div>
        <Link to="/orgs" className="font-sans text-xs text-text-secondary hover:text-primary-text">
          ← {t("orgs.title")}
        </Link>
        {org && (
          <div className="mt-1 flex flex-wrap items-center gap-3">
            <div className="font-display text-2xl font-bold text-text-primary">{org.name}</div>
            <Badge>{org.phase}</Badge>
          </div>
        )}
        {org && (
          <div className="mt-0.5 font-mono text-xs text-text-tertiary">
            {org.slug} · {org.namespace}
          </div>
        )}
      </div>

      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("orgs.detailLoadFailed"))}</div>}

      {org && (
        <>
          <OrgUsage slug={slug} />
          <OrgQuotaSection slug={slug} quota={org.quota} />
          <OrgServers slug={slug} />
          <OrgMembers slug={slug} />
          <OrgDanger slug={slug} name={org.name} />
        </>
      )}
    </div>
  );
}
