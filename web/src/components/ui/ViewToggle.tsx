import clsx from "clsx";
import { useT } from "../../lib/i18n";

export type DashboardView = "list" | "cards";

export function ViewToggle({ value, onChange }: { value: DashboardView; onChange: (v: DashboardView) => void }) {
  const t = useT();
  const item = (v: DashboardView, label: string) => (
    <button
      type="button"
      aria-pressed={value === v}
      onClick={() => onChange(v)}
      className={clsx(
        "px-3 py-1.5 font-sans text-xs",
        value === v ? "bg-surface-hover text-primary-text" : "text-text-tertiary hover:text-text-primary",
      )}
    >
      {label}
    </button>
  );
  return (
    <div className="flex divide-x divide-border border border-border">
      {item("list", t("view.list"))}
      {item("cards", t("view.cards"))}
    </div>
  );
}
