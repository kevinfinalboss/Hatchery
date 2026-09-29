import { useState } from "react";
import { useT } from "../../lib/i18n";
import type { GameServerExtraPort } from "../../lib/types";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";

const PRESETS: GameServerExtraPort[] = [
  { name: "dynmap", containerPort: 8123, protocol: "TCP" },
  { name: "bluemap", containerPort: 8100, protocol: "TCP" },
  { name: "voicechat", containerPort: 24454, protocol: "UDP" },
  { name: "geyser", containerPort: 19132, protocol: "UDP" },
];

const NAME = /^[a-z]([a-z0-9-]{0,13}[a-z0-9])?$/;
export const MAX_EXTRA_PORTS = 5;

function problem(p: GameServerExtraPort, all: GameServerExtraPort[]): "name" | "port" | "duplicate" | null {
  if (!NAME.test(p.name)) return "name";
  if (!Number.isInteger(p.containerPort) || p.containerPort < 1024 || p.containerPort > 65535) return "port";
  if (all.some((o) => o !== p && (o.name === p.name || (o.containerPort === p.containerPort && o.protocol === p.protocol)))) return "duplicate";
  return null;
}

export function extraPortsValid(ports: GameServerExtraPort[]): boolean {
  return ports.length <= MAX_EXTRA_PORTS && ports.every((p) => problem(p, ports) === null);
}

export function ExtraPortsEditor({
  ports,
  onChange,
  disabled,
  addresses,
}: {
  ports: GameServerExtraPort[];
  onChange: (ports: GameServerExtraPort[]) => void;
  disabled: boolean;
  addresses: Record<string, string>;
}) {
  const t = useT();
  const [draft, setDraft] = useState<GameServerExtraPort>({ name: "", containerPort: 0, protocol: "TCP" });
  const draftProblem = draft.name === "" && !draft.containerPort ? null : problem(draft, [...ports, draft]);
  const full = ports.length >= MAX_EXTRA_PORTS;
  const problemText = { name: t("server.extraPortsBadName"), port: t("server.extraPortsBadPort"), duplicate: t("server.extraPortsDuplicate") };

  return (
    <div className="flex flex-col gap-2">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.extraPorts")}</div>
      <span className="font-prose text-xs text-text-tertiary">{t("server.extraPortsHint")}</span>
      {ports.length > 0 && (
        <div className="flex flex-col divide-y divide-border border border-border">
          {ports.map((p) => (
            <div key={p.name} className="flex items-center gap-3 px-3 py-2 font-mono text-sm">
              <span className="text-text-primary">{p.name}</span>
              <span className="text-text-secondary">
                {p.containerPort}/{p.protocol}
              </span>
              {addresses[p.name] && <span className="text-xs text-text-tertiary">→ {addresses[p.name]}</span>}
              {!disabled && (
                <button
                  type="button"
                  className="ml-auto font-sans text-xs text-text-secondary hover:text-status-failed"
                  onClick={() => onChange(ports.filter((o) => o !== p))}
                >
                  {t("server.extraPortsRemove")}
                </button>
              )}
            </div>
          ))}
        </div>
      )}
      {!disabled && !full && (
        <>
          <div className="flex flex-wrap gap-1">
            {PRESETS.filter((pre) => !ports.some((p) => p.name === pre.name)).map((pre) => (
              <button
                key={pre.name}
                type="button"
                className="border border-border px-2 py-0.5 font-sans text-xs text-text-secondary hover:border-border-strong hover:text-text-primary"
                onClick={() => setDraft(pre)}
              >
                {pre.name} {pre.containerPort}/{pre.protocol}
              </button>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              aria-label={t("server.extraPortsName")}
              placeholder={t("server.extraPortsName")}
              className="w-36"
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value.toLowerCase() })}
            />
            <Input
              aria-label={t("server.extraPortsPort")}
              placeholder="8123"
              inputMode="numeric"
              className="w-24"
              value={draft.containerPort || ""}
              onChange={(e) => setDraft({ ...draft, containerPort: Number(e.target.value.replace(/\D/g, "")) })}
            />
            <select
              aria-label={t("server.extraPortsProtocol")}
              value={draft.protocol}
              onChange={(e) => setDraft({ ...draft, protocol: e.target.value as "TCP" | "UDP" })}
              className="rounded-lg border border-border-strong bg-surface px-2 py-2 font-sans text-sm text-text-primary"
            >
              <option value="TCP">TCP</option>
              <option value="UDP">UDP</option>
            </select>
            <Button
              type="button"
              variant="secondary"
              disabled={draft.name === "" || draftProblem !== null}
              onClick={() => {
                onChange([...ports, draft]);
                setDraft({ name: "", containerPort: 0, protocol: "TCP" });
              }}
            >
              {t("server.extraPortsAdd")}
            </Button>
          </div>
          {draftProblem && <span className="font-prose text-xs text-status-failed">{problemText[draftProblem]}</span>}
        </>
      )}
    </div>
  );
}
