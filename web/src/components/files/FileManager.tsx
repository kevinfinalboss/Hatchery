import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { api } from "../../lib/api";
import type { FileEntry } from "../../lib/types";
import { Button } from "../ui/Button";
import { FileEditor } from "./FileEditor";

// Extensions the editor opens as text; anything else (or anything over
// MAX_EDITABLE_SIZE) downloads instead of opening — see the spec's "Limite
// de edição".
const EDITABLE_TEXT_EXTENSIONS = new Set([
  "txt", "json", "yml", "yaml", "properties", "toml", "cfg", "conf",
  "js", "mjs", "cjs", "lua", "sh", "log", "md",
]);
const MAX_EDITABLE_SIZE = 2 * 1024 * 1024;

function isEditable(entry: FileEntry): boolean {
  if (entry.isDir || entry.size > MAX_EDITABLE_SIZE) return false;
  const ext = entry.name.split(".").pop()?.toLowerCase() ?? "";
  return EDITABLE_TEXT_EXTENSIONS.has(ext) || !entry.name.includes(".");
}

function joinPath(dir: string, name: string): string {
  return dir === "/" ? `/${name}` : `${dir}/${name}`;
}

export function FileManager({ namespace, name }: { namespace: string; name: string }) {
  const queryClient = useQueryClient();
  const [currentPath, setCurrentPath] = useState("/");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [editingContent, setEditingContent] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const {
    data: entries,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ["files", namespace, name, currentPath],
    queryFn: () => api.listFiles(namespace, name, currentPath),
    // A Stopped GameServer's first file-manager request triggers an
    // on-demand maintenance Pod server-side (see resolveSFTPTarget) — retry
    // a few times instead of surfacing a hard error while it comes up.
    retry: 3,
    retryDelay: 2000,
  });

  function invalidate() {
    void queryClient.invalidateQueries({ queryKey: ["files", namespace, name] });
  }

  const mkdirMutation = useMutation({
    mutationFn: (dirName: string) => api.mkdir(namespace, name, joinPath(currentPath, dirName)),
    onSuccess: invalidate,
  });
  const deleteMutation = useMutation({
    mutationFn: (paths: string[]) => api.deleteFiles(namespace, name, paths),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
  });
  const renameMutation = useMutation({
    mutationFn: ({ from, to }: { from: string; to: string }) => api.renameFile(namespace, name, from, to),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
  });
  const copyMutation = useMutation({
    mutationFn: ({ from, to }: { from: string; to: string }) => api.copyFile(namespace, name, from, to),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
  });
  const compressMutation = useMutation({
    mutationFn: ({ paths, dest }: { paths: string[]; dest: string }) =>
      api.compressFiles(namespace, name, paths, dest),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
  });
  const decompressMutation = useMutation({
    mutationFn: ({ path, dest }: { path: string; dest: string }) => api.decompressFile(namespace, name, path, dest),
    onSuccess: () => {
      setSelected(new Set());
      invalidate();
    },
  });
  const uploadMutation = useMutation({
    mutationFn: (file: File) => api.uploadFile(namespace, name, currentPath, file),
    onSuccess: invalidate,
  });
  const saveMutation = useMutation({
    mutationFn: ({ path, content }: { path: string; content: string }) =>
      api.putFileContent(namespace, name, path, content),
    onSuccess: () => {
      setEditingPath(null);
      invalidate();
    },
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

  async function openEntry(entry: FileEntry) {
    const fullPath = joinPath(currentPath, entry.name);
    if (entry.isDir) {
      setCurrentPath(fullPath);
      setSelected(new Set());
      return;
    }
    if (!isEditable(entry)) {
      await api.downloadFiles(namespace, name, [fullPath]);
      return;
    }
    const content = await api.getFileContent(namespace, name, fullPath);
    setEditingContent(content);
    setEditingPath(fullPath);
  }

  if (editingPath) {
    return (
      <FileEditor
        key={editingPath}
        path={editingPath}
        content={editingContent}
        saving={saveMutation.isPending}
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
        e.preventDefault();
        setDragOver(true);
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
        Array.from(e.dataTransfer.files).forEach((file) => uploadMutation.mutate(file));
      }}
    >
      <div className="flex flex-wrap items-center gap-1 font-mono text-sm text-text-secondary">
        <button className="hover:text-text-primary" onClick={() => setCurrentPath("/")}>
          /
        </button>
        {breadcrumbs.map((segment, i) => (
          <span key={i} className="flex items-center gap-1">
            <button
              className="hover:text-text-primary"
              onClick={() => setCurrentPath("/" + breadcrumbs.slice(0, i + 1).join("/"))}
            >
              {segment}
            </button>
            {i < breadcrumbs.length - 1 && <span>/</span>}
          </span>
        ))}
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          onClick={() => {
            const dirName = prompt("Nome da nova pasta:");
            if (dirName) mkdirMutation.mutate(dirName);
          }}
        >
          Nova pasta
        </Button>
        <Button variant="secondary" onClick={() => fileInputRef.current?.click()}>
          Upload
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
        {selectedList.length > 0 && (
          <>
            <Button variant="secondary" onClick={() => void api.downloadFiles(namespace, name, selectedList)}>
              Baixar ({selectedList.length})
            </Button>
            <Button
              variant="secondary"
              onClick={() => {
                const dest = prompt("Compactar selecionados em:", joinPath(currentPath, "archive.zip"));
                if (dest) compressMutation.mutate({ paths: selectedList, dest });
              }}
            >
              Compactar
            </Button>
            {selectedList.length === 1 && selectedList[0].endsWith(".zip") && (
              <Button
                variant="secondary"
                onClick={() => {
                  const dest = prompt("Descompactar em:", currentPath);
                  if (dest) decompressMutation.mutate({ path: selectedList[0], dest });
                }}
              >
                Descompactar
              </Button>
            )}
            {selectedList.length === 1 && (
              <Button
                variant="secondary"
                onClick={() => {
                  const from = selectedList[0];
                  const to = prompt("Mover/renomear para:", from);
                  if (to) renameMutation.mutate({ from, to });
                }}
              >
                Mover/Renomear
              </Button>
            )}
            {selectedList.length === 1 && (
              <Button
                variant="secondary"
                onClick={() => {
                  const from = selectedList[0];
                  const to = prompt("Copiar para:", from + ".copy");
                  if (to) copyMutation.mutate({ from, to });
                }}
              >
                Copiar
              </Button>
            )}
            <Button
              variant="danger"
              onClick={() => {
                if (confirm(`Apagar ${selectedList.length} item(ns)?`)) deleteMutation.mutate(selectedList);
              }}
            >
              Apagar
            </Button>
          </>
        )}
      </div>

      <div className="min-h-0 grow overflow-auto rounded-lg border border-border">
        {isLoading ? (
          <div className="p-4 font-sans text-sm text-text-secondary">Carregando…</div>
        ) : isError ? (
          <div className="p-4 font-sans text-sm text-text-secondary">
            Preparando ambiente de arquivos… tentando de novo.
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
                    <td className="px-3 py-2 text-text-tertiary">{new Date(entry.modTime).toLocaleString()}</td>
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
