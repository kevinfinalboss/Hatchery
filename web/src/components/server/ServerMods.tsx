import { useEffect, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import clsx from "clsx";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { useI18n, useT } from "../../lib/i18n";
import { modsApi, type InstalledMod, type InstallResult, type ModSearchResult, type ModsContext } from "../../lib/mods";
import { useRestartServer } from "../../lib/serverActions";
import type { GameServer } from "../../lib/types";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { Input } from "../ui/Input";
import { Badge, ListRow, RowActions } from "../ui/List";
import { Modal } from "../ui/Modal";

type Tab = "installed" | "explore";

function sourceLabel(source: string): string {
  return source === "curseforge" ? "CurseForge" : source === "modrinth" ? "Modrinth" : source;
}

function formatCount(n: number, locale: string): string {
  return new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }).format(n);
}

function Icon({ url, title }: { url?: string; title: string }) {
  if (url) return <img src={url} alt="" className="h-9 w-9 shrink-0 rounded object-cover" loading="lazy" />;
  return (
    <span aria-hidden className="flex h-9 w-9 shrink-0 items-center justify-center rounded bg-surface-hover font-sans text-sm text-text-tertiary">
      {title.slice(0, 1).toUpperCase()}
    </span>
  );
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return v;
}

function friendlyError(err: unknown, t: ReturnType<typeof useT>, fallback: string): string {
  if (err instanceof ApiError && err.status === 429) return t("mods.rateLimited");
  return errorMessage(err, fallback);
}

export function ServerMods({ org, name, server, canWrite }: { org: string; name: string; server: GameServer; canWrite: boolean }) {
  const t = useT();
  const queryClient = useQueryClient();
  const restart = useRestartServer();
  const [params, setParams] = useSearchParams();
  const tab: Tab = params.get("tab") === "explore" ? "explore" : "installed";
  const setTab = (next: Tab) => setParams({ s: "mods", tab: next }, { replace: true });

  const { data: ctx, error: ctxError } = useQuery({ queryKey: ["mods-context", org, name], queryFn: () => modsApi.context(org, name) });
  const [chosenVersion, setChosenVersion] = useState("");
  const gameVersion = ctx?.gameVersion ?? (chosenVersion.trim() || undefined);

  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);
  const running = server.status?.phase === "Running" || server.status?.phase === "Starting";
  const [changed, setChanged] = useState(false);

  function afterChange(text: string) {
    setNotice({ ok: true, text });
    setChanged(true);
    void queryClient.invalidateQueries({ queryKey: ["mods-installed", org, name] });
  }

  if (ctxError) return <div className="font-sans text-sm text-status-failed">{errorMessage(ctxError, t("mods.loadFailed"))}</div>;
  if (!ctx) return <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        {ctx.loaders.map((l) => (
          <Badge key={l}>{l}</Badge>
        ))}
        {ctx.gameVersion ? (
          <Badge>{t("mods.gameVersion", { version: ctx.gameVersion })}</Badge>
        ) : (
          <label className="flex items-center gap-2 font-sans text-xs text-text-secondary">
            {t("mods.gameVersionUnknown")}
            <Input className="w-28 py-1" placeholder="1.21.4" value={chosenVersion} onChange={(e) => setChosenVersion(e.target.value)} />
          </label>
        )}
        <span className="font-mono text-xs text-text-tertiary">/{ctx.directory}</span>
      </div>

      <div className="flex gap-1 border-b border-border">
        {(["installed", "explore"] as const).map((id) => (
          <button
            key={id}
            onClick={() => setTab(id)}
            className={clsx(
              "-mb-px border-b-2 px-3 py-1.5 font-sans text-sm",
              tab === id ? "border-primary text-primary-text" : "border-transparent text-text-secondary hover:text-text-primary",
            )}
          >
            {id === "installed" ? t("mods.installed") : t("mods.explore")}
          </button>
        ))}
      </div>

      {notice && <div className={clsx("font-sans text-sm", notice.ok ? "text-status-running" : "text-status-failed")}>{notice.text}</div>}
      {changed && running && canWrite && (
        <div className="flex flex-wrap items-center justify-between gap-3 border border-border-strong bg-surface px-4 py-2.5 font-sans text-sm text-text-primary">
          <span>{t("mods.restartHint")}</span>
          <Button
            variant="secondary"
            disabled={restart.isPending}
            onClick={() => restart.mutate({ org, name }, { onSuccess: () => setChanged(false) })}
          >
            {t("mods.restartNow")}
          </Button>
        </div>
      )}

      {tab === "installed" ? (
        <InstalledTab org={org} name={name} ctx={ctx} gameVersion={gameVersion} canWrite={canWrite} onChanged={afterChange} onError={(text) => setNotice({ ok: false, text })} />
      ) : (
        <ExploreTab org={org} name={name} ctx={ctx} gameVersion={gameVersion} canWrite={canWrite} onChanged={afterChange} onError={(text) => setNotice({ ok: false, text })} />
      )}
    </div>
  );
}

function installedMessage(t: ReturnType<typeof useT>, r: InstallResult): string {
  const files = r.installed.map((i) => i.file).join(", ");
  let msg = t("mods.installedMsg", { files });
  if (r.skipped.length > 0) msg += ` ${t("mods.skippedMsg", { count: r.skipped.length })}`;
  if (r.warnings && r.warnings.length > 0) msg += ` ${t("mods.dependencyWarnings", { list: r.warnings.join("; ") })}`;
  return msg;
}

interface TabProps {
  org: string;
  name: string;
  ctx: ModsContext;
  gameVersion?: string;
  canWrite: boolean;
  onChanged: (text: string) => void;
  onError: (text: string) => void;
}

function InstalledTab({ org, name, ctx, gameVersion, canWrite, onChanged, onError }: TabProps) {
  const t = useT();
  const { data, isLoading, error } = useQuery({
    queryKey: ["mods-installed", org, name, gameVersion ?? ""],
    queryFn: () => modsApi.installed(org, name, ctx.gameVersion ? undefined : gameVersion),
  });
  const update = useMutation({
    mutationFn: (file: string) => modsApi.update(org, name, file),
    onSuccess: (r) => onChanged(t("mods.updatedMsg", { file: r.file })),
    onError: (err) => onError(friendlyError(err, t, t("mods.actionFailed"))),
  });
  const remove = useMutation({
    mutationFn: (file: string) => modsApi.remove(org, name, file),
    onSuccess: (_r, file) => onChanged(t("mods.removedMsg", { file })),
    onError: (err) => onError(friendlyError(err, t, t("mods.actionFailed"))),
  });
  const [updatingAll, setUpdatingAll] = useState(false);

  async function updateAll(items: InstalledMod[]) {
    setUpdatingAll(true);
    try {
      for (const it of items) await update.mutateAsync(it.file);
    } catch {
    } finally {
      setUpdatingAll(false);
    }
  }

  if (isLoading) return <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>;
  if (error) return <div className="font-sans text-sm text-status-failed">{friendlyError(error, t, t("mods.loadFailed"))}</div>;
  const items = data?.items ?? [];
  const updatable = items.filter((i) => i.updateAvailable);

  return (
    <div className="flex flex-col gap-3">
      {(data?.warnings.length ?? 0) > 0 && <div className="font-sans text-xs text-status-installing">{t("mods.warnings", { list: data!.warnings.join("; ") })}</div>}
      {canWrite && updatable.length > 1 && (
        <div>
          <Button variant="secondary" disabled={updatingAll || update.isPending} onClick={() => void updateAll(updatable)}>
            {t("mods.updateAll", { count: updatable.length })}
          </Button>
        </div>
      )}
      {items.length === 0 ? (
        <div className="font-sans text-sm text-text-secondary">{t("mods.empty")}</div>
      ) : (
        <Card className="divide-y divide-border">
          {items.map((it) => (
            <ListRow key={it.file}>
              <div className="flex min-w-0 items-center gap-3">
                <Icon url={it.iconUrl} title={it.title ?? it.file} />
                <div className="min-w-0">
                  <div className={clsx("truncate font-sans text-sm font-semibold", it.title ? "text-text-primary" : "text-text-tertiary")}>
                    {it.pageUrl ? (
                      <a href={it.pageUrl} target="_blank" rel="noreferrer" className="hover:text-primary-text">
                        {it.title ?? it.file}
                      </a>
                    ) : (
                      (it.title ?? it.file)
                    )}
                  </div>
                  <div className="truncate font-sans text-xs text-text-tertiary">
                    {it.title ? `${it.version ?? ""} · ${sourceLabel(it.source ?? "")} · ${it.file}` : t("mods.unknown")}
                  </div>
                </div>
              </div>
              <RowActions>
                {it.updateAvailable && <Badge>{t("mods.updateAvailable", { version: it.latestVersion ?? "" })}</Badge>}
                {canWrite && it.updateAvailable && (
                  <Button variant="ghost" disabled={update.isPending || updatingAll} onClick={() => update.mutate(it.file)}>
                    {t("mods.update")}
                  </Button>
                )}
                {canWrite && (
                  <Button
                    variant="ghost"
                    disabled={remove.isPending}
                    onClick={() => {
                      if (confirm(t("mods.removeConfirm", { file: it.file }))) remove.mutate(it.file);
                    }}
                  >
                    {t("mods.remove")}
                  </Button>
                )}
              </RowActions>
            </ListRow>
          ))}
        </Card>
      )}
    </div>
  );
}

function ExploreTab({ org, name, ctx, gameVersion, canWrite, onChanged, onError }: TabProps) {
  const t = useT();
  const { locale } = useI18n();
  const [source, setSource] = useState(ctx.sources[0] ?? "modrinth");
  const [query, setQuery] = useState("");
  const q = useDebounced(query, 300);
  const [picking, setPicking] = useState<ModSearchResult | null>(null);

  const search = useInfiniteQuery({
    queryKey: ["mods-search", org, name, source, q, gameVersion ?? ""],
    queryFn: ({ pageParam }) => modsApi.search(org, name, { source, q, gameVersion, page: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => (pages.length * 20 < last.total ? pages.length : undefined),
  });

  const install = useMutation({
    mutationFn: (v: { projectId: string; versionId?: string }) => modsApi.install(org, name, { source, ...v, gameVersion }),
    onSuccess: (r) => {
      setPicking(null);
      onChanged(installedMessage(t, r));
    },
    onError: (err) => onError(friendlyError(err, t, t("mods.actionFailed"))),
  });

  const results = search.data?.pages.flatMap((p) => p.results) ?? [];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {ctx.sources.length > 1 && (
          <div role="group" aria-label={t("mods.source")} className="flex gap-1">
            {ctx.sources.map((s) => (
              <Button key={s} variant={s === source ? "secondary" : "ghost"} onClick={() => setSource(s)}>
                {sourceLabel(s)}
              </Button>
            ))}
          </div>
        )}
        <Input className="min-w-[220px] grow" placeholder={t("mods.search")} value={query} onChange={(e) => setQuery(e.target.value)} />
      </div>

      {search.isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {search.error && <div className="font-sans text-sm text-status-failed">{friendlyError(search.error, t, t("mods.loadFailed"))}</div>}
      {!search.isLoading && !search.error && results.length === 0 && <div className="font-sans text-sm text-text-secondary">{t("mods.noResults")}</div>}

      <div className="grid gap-3 lg:grid-cols-2">
        {results.map((r) => (
          <Card key={`${r.source}-${r.projectId}`} className="flex gap-3 p-4">
            <Icon url={r.iconUrl} title={r.title} />
            <div className="flex min-w-0 grow flex-col gap-1">
              <div className="flex items-baseline gap-2">
                <a href={r.pageUrl} target="_blank" rel="noreferrer" className="truncate font-sans text-sm font-semibold text-text-primary hover:text-primary-text">
                  {r.title}
                </a>
                <span className="shrink-0 font-sans text-xs text-text-tertiary">{t("mods.by", { author: r.author })}</span>
              </div>
              <div className="line-clamp-2 font-prose text-xs text-text-secondary">{r.description}</div>
              <div className="mt-1 flex flex-wrap items-center gap-2">
                <span className="font-sans text-xs text-text-tertiary">
                  {t("mods.downloads", { count: formatCount(r.downloads, locale) })} · {sourceLabel(r.source)}
                </span>
                <span className="grow" />
                {r.distributionBlocked ? (
                  <>
                    <Badge>{t("mods.blocked")}</Badge>
                    <a href={r.pageUrl} target="_blank" rel="noreferrer" className="font-sans text-xs text-primary-text hover:underline">
                      {t("mods.openPage")}
                    </a>
                  </>
                ) : (
                  canWrite && (
                    <>
                      <Button variant="ghost" onClick={() => setPicking(r)}>
                        {t("mods.chooseVersion")}
                      </Button>
                      <Button disabled={install.isPending} onClick={() => install.mutate({ projectId: r.projectId })}>
                        {install.isPending && install.variables?.projectId === r.projectId ? t("mods.installing") : t("mods.install")}
                      </Button>
                    </>
                  )
                )}
              </div>
            </div>
          </Card>
        ))}
      </div>

      {search.hasNextPage && (
        <div>
          <Button variant="secondary" disabled={search.isFetchingNextPage} onClick={() => void search.fetchNextPage()}>
            {t("mods.loadMore")}
          </Button>
        </div>
      )}

      {picking && (
        <VersionPicker
          org={org}
          name={name}
          result={picking}
          gameVersion={gameVersion}
          installing={install.isPending}
          onClose={() => setPicking(null)}
          onInstall={(versionId) => install.mutate({ projectId: picking.projectId, versionId })}
        />
      )}
    </div>
  );
}

function VersionPicker({
  org,
  name,
  result,
  gameVersion,
  installing,
  onClose,
  onInstall,
}: {
  org: string;
  name: string;
  result: ModSearchResult;
  gameVersion?: string;
  installing: boolean;
  onClose: () => void;
  onInstall: (versionId: string) => void;
}) {
  const t = useT();
  const { formatDateTime } = useI18n();
  const { data, isLoading, error } = useQuery({
    queryKey: ["mods-versions", org, name, result.source, result.projectId, gameVersion ?? ""],
    queryFn: () => modsApi.versions(org, name, result.source, result.projectId, gameVersion),
  });

  return (
    <Modal title={t("mods.versionsOf", { title: result.title })} onClose={onClose}>
      {isLoading && <div className="font-sans text-sm text-text-secondary">{t("common.loading")}</div>}
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, t("mods.loadFailed"))}</div>}
      {data && data.length === 0 && <div className="font-sans text-sm text-text-secondary">{t("mods.noVersions")}</div>}
      <div className="flex max-h-[60vh] flex-col divide-y divide-border overflow-y-auto">
        {data?.map((v) => (
          <div key={v.id} className="flex items-center justify-between gap-3 py-2">
            <div className="min-w-0">
              <div className="truncate font-sans text-sm text-text-primary">{v.versionNumber || v.name}</div>
              <div className="truncate font-sans text-xs text-text-tertiary">
                {(v.gameVersions ?? []).slice(0, 4).join(", ")} · {formatDateTime(v.publishedAt)}
              </div>
            </div>
            {v.distributionBlocked ? (
              <Badge>{t("mods.blocked")}</Badge>
            ) : (
              <Button variant="secondary" disabled={installing} onClick={() => onInstall(v.id)}>
                {t("mods.install")}
              </Button>
            )}
          </div>
        ))}
      </div>
    </Modal>
  );
}
