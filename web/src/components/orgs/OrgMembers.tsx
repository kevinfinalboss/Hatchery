import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useT, type TKey } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import { Card } from "../ui/Card";
import { Badge } from "../ui/List";

const ROLE_KEY: Record<string, TKey> = {
  owner: "members.roleOwner",
  admin: "members.roleAdmin",
  member: "members.roleMember",
};

export function OrgMembers({ slug }: { slug: string }) {
  const t = useT();
  const { data, error } = useQuery({ queryKey: ["members", slug], queryFn: () => api.listMembers(slug) });

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("orgs.membersTitle")}</div>
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("members.loadFailed"))}</div>}
      {!error && data && data.length === 0 && <div className="font-prose text-sm text-text-tertiary">{t("orgs.noMembers")}</div>}
      <div className="divide-y divide-border">
        {(data ?? []).map((m) => (
          <div key={m.userId} className="flex items-center justify-between gap-3 py-2">
            <span className="font-sans text-sm text-text-primary">{m.username}</span>
            <Badge>{ROLE_KEY[m.role] ? t(ROLE_KEY[m.role]) : m.role}</Badge>
          </div>
        ))}
      </div>
    </Card>
  );
}
