import { useEffect, type ReactNode } from "react";
import clsx from "clsx";
import { useT } from "../../lib/i18n";

export function Modal({
  title,
  onClose,
  wide,
  children,
}: {
  title: string;
  onClose: () => void;
  wide?: boolean;
  children: ReactNode;
}) {
  const t = useT();
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 px-4 py-16"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={clsx("w-full border border-border-strong bg-surface", wide ? "max-w-2xl" : "max-w-lg")}
      >
        <div className="flex items-center justify-between border-b border-border px-5 py-3">
          <span className="font-display text-sm font-bold text-text-primary">{title}</span>
          <button onClick={onClose} aria-label={t("common.close")} className="text-text-tertiary hover:text-text-primary">
            ✕
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  );
}
