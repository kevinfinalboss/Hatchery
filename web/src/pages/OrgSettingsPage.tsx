import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useT } from "../lib/i18n";
import { errorMessage } from "../lib/errors";
import { atLeast, useOrg } from "../lib/org";
import { BackupConnections } from "../components/orgs/BackupConnections";
import { NotificationSettingsCard } from "../components/orgs/NotificationSettings";
import { DiscordGuildCard } from "../components/DiscordConnect";

export function OrgSettingsPage() {
  const t = useT();
  const { current } = useOrg();
  const org = current?.slug ?? "";
  const { data: features } = useQuery({ queryKey: ["auth-features"], queryFn: api.features });
  const { data: settings, error } = useQuery({
    queryKey: ["backup-settings", org],
    queryFn: () => api.getBackupSettings(org),
    enabled: !!org,
  });

  return (
    <div className="flex max-w-3xl flex-col gap-5">
      <div>
        <div className="font-display text-2xl font-bold text-text-primary">{t("orgSettings.title")}</div>
        {current && <div className="mt-0.5 font-sans text-sm text-text-secondary">{current.name}</div>}
      </div>

      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("orgSettings.backups")}</div>
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("backups.loadFailed"))}</div>}
      {settings && <BackupConnections org={org} settings={settings} canManage={atLeast(current?.role, "admin")} />}

      {atLeast(current?.role, "admin") && (
        <>
          <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("orgSettings.notifications")}</div>
          <NotificationSettingsCard key={org} org={org} />
        </>
      )}

      {features?.discord && atLeast(current?.role, "admin") && (
        <>
          <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">Discord</div>
          <DiscordGuildCard
            key={org}
            queryKey={["org-discord", org]}
            load={() => api.getOrgDiscord(org)}
            authorize={() => api.orgDiscordURL(org)}
            disconnect={() => api.disconnectOrgDiscord(org)}
            help={t("discord.orgHelp")}
            commands={[
              { name: "/servidores · /status", permission: t("discord.permAnyGrant") },
              { name: "/iniciar · /parar · /reiniciar", permission: "power" },
              { name: "/comando", permission: "console.write" },
              { name: "/backup", permission: "backups.manage" },
            ]}
          />
        </>
      )}
    </div>
  );
}
