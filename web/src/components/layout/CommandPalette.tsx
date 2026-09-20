import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { useT } from "../../lib/i18n";
import { type Command, filterCommands, useCommands } from "../../lib/commands";

function PaletteBody({ onClose }: { onClose: () => void }) {
  const t = useT();
  const commands = useCommands();
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const results = useMemo(() => filterCommands(commands, query), [commands, query]);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  function run(cmd: Command | undefined) {
    if (!cmd) return;
    onClose();
    cmd.run();
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setIndex((i) => Math.min(i + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      run(results[index]);
    } else if (e.key === "Escape") {
      onClose();
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 px-4 pt-[15vh]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div role="dialog" aria-modal="true" aria-label={t("palette.dialog")} className="w-full max-w-lg border border-border-strong bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <span className="text-primary-text" aria-hidden>
            &gt;
          </span>
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setIndex(0);
            }}
            onKeyDown={onKeyDown}
            placeholder={t("palette.placeholder")}
            className="w-full bg-transparent font-sans text-sm text-text-primary placeholder:text-text-tertiary focus:outline-none"
            spellCheck={false}
            autoComplete="off"
          />
        </div>
        <div className="max-h-[50vh] overflow-y-auto py-1">
          {results.length === 0 && <div className="px-4 py-3 font-sans text-sm text-text-tertiary">{t("palette.empty")}</div>}
          {results.map((cmd, i) => (
            <button
              key={cmd.id}
              onMouseEnter={() => setIndex(i)}
              onClick={() => run(cmd)}
              className={clsx(
                "flex w-full items-center justify-between gap-4 px-4 py-2 text-left font-sans text-sm",
                i === index ? "bg-surface-hover text-primary-text" : "text-text-primary",
              )}
            >
              <span className="truncate">{cmd.label}</span>
              <span className="shrink-0 text-xs text-text-tertiary">{cmd.hint ?? cmd.group}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  if (!open) return null;
  return <PaletteBody onClose={onClose} />;
}
