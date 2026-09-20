import { useT } from "../../lib/i18n";
import { useTheme } from "../../lib/theme";

export function ThemeToggle() {
  const { theme, toggle } = useTheme();
  const t = useT();
  return (
    <button
      onClick={toggle}
      aria-label={theme === "dark" ? t("theme.toLight") : t("theme.toDark")}
      title={theme === "dark" ? t("theme.light") : t("theme.dark")}
      className="font-sans text-sm text-text-tertiary hover:text-text-primary"
    >
      {theme === "dark" ? "☀" : "☾"}
    </button>
  );
}
