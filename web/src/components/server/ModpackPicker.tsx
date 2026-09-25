import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { errorMessage } from "../../lib/errors";
import { useI18n, useT } from "../../lib/i18n";
import type { ModSearchResult } from "../../lib/mods";
import { modpacksApi, type ModpackVersion } from "../../lib/modpacks";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";

export interface PickedModpack {
  source: string;
  result: ModSearchResult;
  version: ModpackVersion;
}

const SOURCES = ["modrinth", "curseforge"] as const;

function sourceLabel(s: string) {
  return s === "curseforge" ? "CurseForge" : "Modrinth";
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return v;
}

export function ModpackPicker({ org, picked, onPick }: { org: string; picked: PickedModpack | null; onPick: (p: PickedModpack | null) => void }) {
  const t = useT();
  const { locale, formatDateTime } = useI18n();
  const [source, setSource] = useState<string>("modrinth");
  const [query, setQuery] = useState("");
  const q = useDebounced(query, 300);
  const [open, setOpen] = useState<ModSearchResult | null>(null);

  const search = useQuery({
    queryKey: ["modpack-search", org, source, q],
    queryFn: () => modpacksApi.search(org, source, q),
    enabled: !picked,
  });
  const versions = useQuery({
    queryKey: ["modpack-versions", org, source, open?.projectId],
    queryFn: () => modpacksApi.versions(org, source, open!.projectId),
    enabled: !!open,
  });

  if (picked) {
    return (
      <div className="flex flex-wrap items-center gap-3 border border-border bg-canvas p-3">
        {picked.result.iconUrl && <img src={picked.result.iconUrl} alt="" className="h-10 w-10 rounded object-cover" />}
        <div className="min-w-0 grow">
          <div className="font-sans text-sm font-semibold text-text-primary">{picked.result.title}</div>
          <div className="font-sans text-xs text-text-tertiary">
            {picked.version.versionNumber || picked.version.name} · Minecraft {picked.version.gameVersion} · {(picked.version.loaders ?? []).join(", ")} ·{" "}
            {sourceLabel(picked.source)}
          </div>
        </div>
        <Button type="button" variant="ghost" onClick={() => onPick(null)}>
          {t("modpacks.change")}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <div role="group" aria-label={t("mods.source")} className="flex gap-1">
          {SOURCES.map((s) => (
            <Button
              key={s}
              type="button"
              variant={s === source ? "secondary" : "ghost"}
              onClick={() => {
                setSource(s);
                setOpen(null);
              }}
            >
              {sourceLabel(s)}
            </Button>
          ))}
        </div>
        <Input className="min-w-[220px] grow" placeholder={t("modpacks.search")} value={query} onChange={(e) => setQuery(e.target.value)} />
      </div>
      {search.isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {search.error && <div className="font-sans text-sm text-status-failed">{errorMessage(search.error, t("mods.loadFailed"))}</div>}
      <div className="flex max-h-[420px] flex-col divide-y divide-border overflow-y-auto border border-border">
        {(search.data?.results ?? []).map((r) => (
          <div key={r.projectId}>
            <button
              type="button"
              onClick={() => setOpen(open?.projectId === r.projectId ? null : r)}
              className={clsx("flex w-full items-center gap-3 p-3 text-left hover:bg-surface-hover", open?.projectId === r.projectId && "bg-surface-hover")}
            >
              {r.iconUrl ? (
                <img src={r.iconUrl} alt="" className="h-9 w-9 shrink-0 rounded object-cover" loading="lazy" />
              ) : (
                <span className="h-9 w-9 shrink-0 rounded bg-surface-hover" />
              )}
              <div className="min-w-0 grow">
                <div className="truncate font-sans text-sm font-semibold text-text-primary">{r.title}</div>
                <div className="line-clamp-1 font-prose text-xs text-text-secondary">{r.description}</div>
              </div>
              <span className="shrink-0 font-sans text-xs text-text-tertiary">
                {t("mods.downloads", { count: new Intl.NumberFormat(locale, { notation: "compact" }).format(r.downloads) })}
              </span>
            </button>
            {open?.projectId === r.projectId && (
              <div className="flex flex-col gap-1 bg-canvas px-3 pb-3">
                {versions.isLoading && <div className="font-sans text-xs text-text-secondary">{t("common.loading")}</div>}
                {versions.error && <div className="font-sans text-xs text-status-failed">{errorMessage(versions.error, t("mods.loadFailed"))}</div>}
                {(versions.data ?? []).slice(0, 15).map((v) => (
                  <div key={v.id} className="flex items-center justify-between gap-3 py-1">
                    <div className="min-w-0 font-sans text-xs text-text-secondary">
                      <span className="text-text-primary">{v.versionNumber || v.name}</span> · Minecraft {v.gameVersion} · {(v.loaders ?? []).join(", ")} ·{" "}
                      {formatDateTime(v.publishedAt)}
                    </div>
                    <Button type="button" variant="secondary" onClick={() => onPick({ source, result: r, version: v })}>
                      {t("modpacks.use")}
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
