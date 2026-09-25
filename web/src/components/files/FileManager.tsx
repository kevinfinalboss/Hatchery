import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { useI18n } from "../../lib/i18n";
import { ApiError, api } from "../../lib/api";
import type { FileEntry } from "../../lib/types";
import { Button } from "../ui/Button";
import { FileEditor } from "./FileEditor";

const EDITABLE_TEXT_EXTENSIONS = new Set([
  "txt", "json", "yml", "yaml", "properties", "toml", "cfg", "conf",
  "js", "mjs", "cjs", "lua", "sh", "log", "md",
]);
const MAX_EDITABLE_SIZE = 2 * 1024 * 1024;

function isEditable(entry: FileEntry): boolean {
  if (entry.isDir || entry.size > MAX_EDITABLE_SIZE) return false;
  const ext = entry.name.split(".").pop()?.toLowerCase() ?? "";
  // No extensionless fallback: getFileContent reads via res.text(), which
  // lossily UTF-8-decodes arbitrary bytes, and saving would write that
  // corruption back over the real file. Extensionless files download instead.
  return EDITABLE_TEXT_EXTENSIONS.has(ext);
}

function joinPath(dir: string, name: string): string {
  return dir === "/" ? `/${name}` : `${dir}/${name}`;
}

export function FileManager({ org, name, readOnly }: { org: string; name: string; readOnly: boolean }) {
  const { t, plural, formatDateTime } = useI18n();
  const queryClient = useQueryClient();
  const [currentPath, setCurrentPath] = useState("/");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [editingContent, setEditingContent] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Every mutation and every async row action funnels failures here; without
  // it a 403/404/409/502 was completely silent to the user.
  function reportError(err: unknown) {
    setError(err instanceof ApiError ? err.message : String(err));
  }

  const {
    data: entries,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ["files", org, name, currentPath],
    queryFn: () => api.listFiles(org, name, currentPath),
    // A Stopped GameServer's first file-manager request triggers an
    // on-demand maintenance Pod server-side (see resolveSFTPTarget) — retry
    // a few times instead of surfacing a hard error while it comes up.
    retry: 3,
    retryDelay: 2000,
  });

  function invalidate() {
    void queryClient.invalidateQueries({ queryKey: ["files", org, name] });
  }

  const mkdirMutation = useMutation({
    mutationFn: (dirName: string) => api.mkdir(org, name, joinPath(currentPath, dirName)),
    onSuccess: invalidate,
    onError: reportError,
  });
  const deleteMutation = useMutation({
    mutationFn: (paths: string[]) => api.deleteFiles(org, name, paths),
    onSuccess: (result) => {
      setSelected(new Set());
      invalidate();
      if (result && result.failed.length > 0) {
        setError(
          t("files.deletePartial", {
            deleted: result.deleted.length,
            total: result.deleted.length + result.failed.length,
            failed: result.failed.map((f) => `${f.path} (${f.error})`).join("; "),
          }),
        );
      }
    },
    onError: reportError,
  });
  const renameMutation = useMutation({
    mutationFn: ({ from, to }: { from: string; to: string }) => api.renameFile(org, name, from, to),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
    onError: reportError,
  });
  const copyMutation = useMutation({
    mutationFn: ({ from, to }: { from: string; to: string }) => api.copyFile(org, name, from, to),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
    onError: reportError,
  });
  const compressMutation = useMutation({
    mutationFn: ({ paths, dest }: { paths: string[]; dest: string }) =>
      api.compressFiles(org, name, paths, dest),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
    onError: reportError,
  });
  const decompressMutation = useMutation({
    mutationFn: ({ path, dest }: { path: string; dest: string }) => api.decompressFile(org, name, path, dest),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
    onError: reportError,
  });
  const uploadMutation = useMutation({
    mutationFn: (file: File) => api.uploadFile(org, name, currentPath, file),
    onSuccess: invalidate,
    onError: reportError,
  });
  const saveMutation = useMutation({
    mutationFn: ({ path, content }: { path: string; content: string }) =>
      api.putFileContent(org, name, path, content),
    onSuccess: () => {
      setEditingPath(null);
      invalidate();
    },
    onError: reportError,
  });

  const breadcrumbs = currentPath === "/" ? [] : currentPath.split("/").filter(Boolean);

  function toggleSelected(path: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }

  // Every path change clears the selection: the checkboxes don't carry over
  // to the new listing, but the bulk toolbar (Apagar included) would still
  // act on the now-invisible paths.
  function navigateTo(p: string) {
    setCurrentPath(p);
    setSelected(new Set());
  }

  async function openEntry(entry: FileEntry) {
    const fullPath = joinPath(currentPath, entry.name);
    if (entry.isDir) {
      navigateTo(fullPath);
      return;
    }
    try {
      if (!isEditable(entry)) {
        await api.downloadFiles(org, name, [fullPath]);
        return;
      }
      const content = await api.getFileContent(org, name, fullPath);
      setEditingContent(content);
      setEditingPath(fullPath);
    } catch (err) {
      reportError(err);
    }
  }

  if (editingPath) {
    return (
      <FileEditor
        key={editingPath}
        path={editingPath}
        content={editingContent}
        saving={saveMutation.isPending}
        readOnly={readOnly}
        onSave={(content) => saveMutation.mutate({ path: editingPath, content })}
        onClose={() => setEditingPath(null)}
      />
    );
  }

  const selectedList = Array.from(selected);

  return (
    <div
      className={clsx("flex h-full flex-col gap-3 rounded-lg", dragOver && "outline-2 outline-dashed outline-primary")}
      onDragOver={(e) => {
        if (readOnly) return;
        e.preventDefault();
        setDragOver(true);
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        if (readOnly) return;
        e.preventDefault();
        setDragOver(false);
        Array.from(e.dataTransfer.files).forEach((file) => uploadMutation.mutate(file));
      }}
    >
      <div className="flex flex-wrap items-center gap-1 font-mono text-sm text-text-secondary">
        <button className="hover:text-text-primary" onClick={() => navigateTo("/")}>
          /
        </button>
        {breadcrumbs.map((segment, i) => (
          <span key={i} className="flex items-center gap-1">
            <button
              className="hover:text-text-primary"
              onClick={() => navigateTo("/" + breadcrumbs.slice(0, i + 1).join("/"))}
            >
              {segment}
            </button>
            {i < breadcrumbs.length - 1 && <span>/</span>}
          </span>
        ))}
      </div>

      {error && (
        <div className="flex items-start justify-between gap-3 rounded-lg border border-status-failed px-3 py-2 font-sans text-sm text-status-failed">
          <span>{error}</span>
          <button
            type="button"
            aria-label={t("files.closeError")}
            className="shrink-0 leading-none hover:text-text-primary"
            onClick={() => setError(null)}
          >
            ✕
          </button>
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        {!readOnly && (
          <>
            <Button
              variant="secondary"
              onClick={() => {
                const dirName = prompt(t("files.newFolderPrompt"));
                if (dirName) mkdirMutation.mutate(dirName);
              }}
            >
              {t("files.newFolder")}
            </Button>
            <Button variant="secondary" onClick={() => fileInputRef.current?.click()}>
              {t("files.upload")}
            </Button>
            <input
              ref={fileInputRef}
              type="file"
              multiple
              className="hidden"
              onChange={(e) => {
                Array.from(e.target.files ?? []).forEach((file) => uploadMutation.mutate(file));
                e.target.value = "";
              }}
            />
          </>
        )}
        {selectedList.length > 0 && (
          <>
            <Button
              variant="secondary"
              onClick={() => void api.downloadFiles(org, name, selectedList).catch(reportError)}
            >
              {t("files.download", { count: selectedList.length })}
            </Button>
            {!readOnly && (
              <>
                <Button
                  variant="secondary"
                  onClick={() => {
                    const dest = prompt(t("files.compressPrompt"), joinPath(currentPath, "archive.zip"));
                    if (dest) compressMutation.mutate({ paths: selectedList, dest });
                  }}
                >
                  {t("files.compress")}
                </Button>
                {selectedList.length === 1 && selectedList[0].endsWith(".zip") && (
                  <Button
                    variant="secondary"
                    onClick={() => {
                      const dest = prompt(t("files.decompressPrompt"), currentPath);
                      if (dest) decompressMutation.mutate({ path: selectedList[0], dest });
                    }}
                  >
                    {t("files.decompress")}
                  </Button>
                )}
                {selectedList.length === 1 && (
                  <Button
                    variant="secondary"
                    onClick={() => {
                      const from = selectedList[0];
                      const to = prompt(t("files.movePrompt"), from);
                      if (to) renameMutation.mutate({ from, to });
                    }}
                  >
                    {t("files.move")}
                  </Button>
                )}
                {selectedList.length === 1 && (
                  <Button
                    variant="secondary"
                    onClick={() => {
                      const from = selectedList[0];
                      const to = prompt(t("files.copyPrompt"), from + ".copy");
                      if (to) copyMutation.mutate({ from, to });
                    }}
                  >
                    {t("files.copy")}
                  </Button>
                )}
                <Button
                  variant="danger"
                  onClick={() => {
                    if (confirm(plural("files.deleteConfirm", selectedList.length))) deleteMutation.mutate(selectedList);
                  }}
                >
                  {t("files.delete")}
                </Button>
              </>
            )}
          </>
        )}
      </div>

      <div className="min-h-0 grow overflow-auto rounded-lg border border-border">
        {isLoading ? (
          <div className="p-4 font-sans text-sm text-text-secondary">{t("common.loading")}</div>
        ) : isError ? (
          <div className="p-4 font-sans text-sm text-text-secondary">
            {t("files.preparing")}
          </div>
        ) : (
          <table className="w-full text-left font-sans text-sm">
            <tbody>
              {(entries ?? []).map((entry) => {
                const fullPath = joinPath(currentPath, entry.name);
                return (
                  <tr key={entry.name} className="border-b border-border hover:bg-surface-hover">
                    <td className="w-8 px-3 py-2">
                      <input
                        type="checkbox"
                        checked={selected.has(fullPath)}
                        onChange={() => toggleSelected(fullPath)}
                      />
                    </td>
                    <td className="cursor-pointer px-3 py-2 text-text-primary" onClick={() => void openEntry(entry)}>
                      {entry.isDir ? "📁" : "📄"} {entry.name}
                    </td>
                    <td className="px-3 py-2 text-text-tertiary">{entry.isDir ? "" : entry.size}</td>
                    <td className="px-3 py-2 text-text-tertiary">{formatDateTime(entry.modTime)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
