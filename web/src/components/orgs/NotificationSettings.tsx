import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { useT } from "../../lib/i18n";
import type { NotificationEvent, NotificationTestResult } from "../../lib/types";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { Field, Input } from "../ui/Input";

const EVENT_KEYS = {
  "gameserver.gave_up": "notifications.eventGaveUp",
  "backup.failed": "notifications.eventBackupFailed",
  "backup.resume_failed": "notifications.eventResumeFailed",
  "schedule.failed": "notifications.eventScheduleFailed",
  "gameserver.suspended": "notifications.eventSuspended",
} as const;

function ChannelResult({ label, value }: { label: string; value: string }) {
  const t = useT();
  const ok = value === "sent";
  const text =
    value === "sent"
      ? t("notifications.resultSent")
      : value === "notConfigured"
        ? t("notifications.resultNotConfigured")
        : value === "noRecipients"
          ? t("notifications.resultNoRecipients")
          : value;
  return (
    <div className="font-sans text-sm">
      <span className="text-text-secondary">{label}: </span>
      <span className={ok ? "text-status-running" : value.startsWith("failed") ? "text-status-failed" : "text-text-secondary"}>{text}</span>
    </div>
  );
}

export function NotificationSettingsCard({ org }: { org: string }) {
  const t = useT();
  const queryClient = useQueryClient();
  const { data, error } = useQuery({ queryKey: ["notifications", org], queryFn: () => api.getNotifications(org), enabled: !!org });
  const [webhook, setWebhook] = useState("");
  const [mutedEdit, setMutedEdit] = useState<NotificationEvent[] | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [testResult, setTestResult] = useState<NotificationTestResult | null>(null);

  const muted = mutedEdit ?? data?.muted ?? [];
  const save = useMutation({
    mutationFn: (body: { discordWebhookUrl?: string; muted: NotificationEvent[] }) => api.putNotifications(org, body),
    onSuccess: (next) => {
      queryClient.setQueryData(["notifications", org], next);
      setWebhook("");
      setMutedEdit(null);
      setMessage({ ok: true, text: t("notifications.saved") });
    },
    onError: (err) => setMessage({ ok: false, text: errorMessage(err, t("notifications.saveFailed")) }),
  });
  const test = useMutation({
    mutationFn: () => api.testNotifications(org),
    onSuccess: setTestResult,
    onError: (err) => setMessage({ ok: false, text: errorMessage(err, t("notifications.testFailed")) }),
  });

  if (error) return <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("notifications.loadFailed"))}</div>;
  if (!data) return null;

  const toggle = (e: NotificationEvent, on: boolean) => setMutedEdit(on ? muted.filter((m) => m !== e) : [...muted, e]);

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-prose text-sm text-text-secondary">{t("notifications.help")}</div>
      <Field label={t("notifications.discordWebhook")} htmlFor="notify-webhook">
        <div className="flex gap-2">
          <Input
            id="notify-webhook"
            type="url"
            className="grow"
            value={webhook}
            placeholder={data.discordConfigured ? t("notifications.discordConfigured") : "https://discord.com/api/webhooks/…"}
            onChange={(e) => setWebhook(e.target.value.trim())}
          />
          {data.discordConfigured && (
            <Button variant="secondary" disabled={save.isPending} onClick={() => save.mutate({ discordWebhookUrl: "", muted })}>
              {t("notifications.discordRemove")}
            </Button>
          )}
        </div>
      </Field>
      <div className="flex flex-col gap-2">
        <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("notifications.events")}</div>
        {data.events.map((e) => (
          <label key={e} className="flex items-center gap-2 font-prose text-sm text-text-primary">
            <input type="checkbox" checked={!muted.includes(e)} onChange={(ev) => toggle(e, ev.target.checked)} />
            {t(EVENT_KEYS[e])}
          </label>
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button
          disabled={save.isPending}
          onClick={() => {
            setMessage(null);
            save.mutate(webhook ? { discordWebhookUrl: webhook, muted } : { muted });
          }}
        >
          {save.isPending ? t("common.saving") : t("common.save")}
        </Button>
        <Button
          variant="secondary"
          disabled={test.isPending}
          onClick={() => {
            setMessage(null);
            setTestResult(null);
            test.mutate();
          }}
        >
          {test.isPending ? t("notifications.testing") : t("notifications.test")}
        </Button>
        {message && <span className={`font-sans text-sm ${message.ok ? "text-status-running" : "text-status-failed"}`}>{message.text}</span>}
      </div>
      {testResult && (
        <div className="flex flex-col gap-1 border border-border px-3 py-2">
          <ChannelResult label="Discord" value={testResult.discord} />
          <ChannelResult label={t("notifications.email")} value={testResult.email} />
        </div>
      )}
    </Card>
  );
}
