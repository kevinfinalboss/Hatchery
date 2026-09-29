import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { errorMessage } from "../lib/errors";
import { useT } from "../lib/i18n";
import type { DiscordConnection } from "../lib/types";
import { Button } from "./ui/Button";
import { Card } from "./ui/Card";

export function useDiscordResult(): string | null {
  const t = useT();
  const [params] = useSearchParams();
  const result = params.get("discord");
  if (!result) return null;
  const known = { linked: "discord.resultLinked", connected: "discord.resultConnected", taken: "discord.resultTaken" } as const;
  return result in known ? t(known[result as keyof typeof known]) : t("discord.resultError");
}

export interface BotCommand {
  name: string;
  permission: string;
}

export function DiscordGuildCard({
  queryKey,
  load,
  authorize,
  disconnect,
  commands,
  help,
}: {
  queryKey: string[];
  load: () => Promise<DiscordConnection>;
  authorize: () => Promise<{ url: string }>;
  disconnect: () => Promise<void>;
  commands: BotCommand[];
  help: string;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const result = useDiscordResult();
  const [error, setError] = useState<string | null>(null);
  const { data } = useQuery({ queryKey, queryFn: load });
  const connect = useMutation({
    mutationFn: authorize,
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (err) => setError(errorMessage(err, t("discord.failed"))),
  });
  const remove = useMutation({
    mutationFn: disconnect,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey }),
    onError: (err) => setError(errorMessage(err, t("discord.failed"))),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-prose text-sm text-text-secondary">{help}</div>
      {result && <div className="font-sans text-sm text-text-primary">{result}</div>}
      {data?.connected ? (
        <div className="flex flex-wrap items-center gap-3">
          <span className="font-sans text-sm text-text-primary">{t("discord.connectedTo", { name: data.guildName || "Discord" })}</span>
          <Button variant="secondary" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t("discord.disconnect")}
          </Button>
        </div>
      ) : (
        <div>
          <Button disabled={connect.isPending || !data} onClick={() => connect.mutate()}>
            {t("discord.addToDiscord")}
          </Button>
        </div>
      )}
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
      <div className="flex flex-col gap-1">
        <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("discord.commands")}</div>
        {commands.map((c) => (
          <div key={c.name} className="flex gap-3 font-mono text-xs">
            <span className="text-text-primary">{c.name}</span>
            <span className="text-text-tertiary">{c.permission}</span>
          </div>
        ))}
      </div>
    </Card>
  );
}
