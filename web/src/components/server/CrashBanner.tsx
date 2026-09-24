import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { crashNotice } from "../../lib/gameserver";
import { useT } from "../../lib/i18n";
import type { GameServer } from "../../lib/types";
import { Button } from "../ui/Button";
import { Modal } from "../ui/Modal";

export function CrashBanner({ org, server, canReadLog }: { org: string; server: GameServer; canReadLog: boolean }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const notice = crashNotice(server);
  const name = server.metadata.name;
  const { data, isLoading, error } = useQuery({
    queryKey: ["crash-log", org, name, notice?.crash.at],
    queryFn: () => api.getCrashLog(org, name),
    enabled: open,
  });
  if (!notice) return null;

  const time = new Date(notice.crash.at).toLocaleString();
  const cause = notice.crash.oomKilled ? t("server.crashCauseOOM") : t("server.crashCauseExit", { code: String(notice.crash.exitCode) });
  const gaveUp = notice.gaveUp
    ? notice.reason === "AutoRestartDisabled"
      ? t("server.crashGaveUpDisabled")
      : t("server.crashGaveUpLoop")
    : t("server.crashRecovered");

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border border-status-failed bg-surface px-4 py-2.5 font-sans text-sm text-status-failed">
      <span>
        {t("server.crashBanner", { time, cause })} {gaveUp}
      </span>
      {canReadLog && (
        <Button variant="secondary" onClick={() => setOpen(true)}>
          {t("server.crashViewLog")}
        </Button>
      )}
      {open && (
        <Modal title={t("server.crashLogTitle")} wide onClose={() => setOpen(false)}>
          {isLoading ? (
            <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>
          ) : error ? (
            <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("server.crashLogUnavailable"))}</div>
          ) : (
            <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-all font-mono text-xs text-text-primary">
              {data?.log || t("server.crashLogEmpty")}
            </pre>
          )}
        </Modal>
      )}
    </div>
  );
}
