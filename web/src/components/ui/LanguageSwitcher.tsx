import type { JSX } from "react";
import clsx from "clsx";
import { LOCALES, type Locale, useI18n } from "../../lib/i18n";
import { useAuth } from "../../lib/auth";
import { api } from "../../lib/api";

function FlagBR() {
  return (
    <svg viewBox="0 0 28 20" className="h-full w-full" aria-hidden>
      <rect width="28" height="20" fill="#009c3b" />
      <polygon points="14,2 26,10 14,18 2,10" fill="#ffdf00" />
      <circle cx="14" cy="10" r="4.2" fill="#002776" />
    </svg>
  );
}

function FlagUS() {
  const stripe = 20 / 13;
  return (
    <svg viewBox="0 0 28 20" className="h-full w-full" aria-hidden>
      <rect width="28" height="20" fill="#ffffff" />
      {Array.from({ length: 7 }, (_, i) => (
        <rect key={i} y={i * 2 * stripe} width="28" height={stripe} fill="#b22234" />
      ))}
      <rect width="11.2" height={stripe * 7} fill="#3c3b6e" />
    </svg>
  );
}

const flags: Record<Locale, () => JSX.Element> = { "pt-BR": FlagBR, en: FlagUS };

export function LanguageSwitcher({ className }: { className?: string }) {
  const { locale, setLocale, t } = useI18n();
  const { user, setUser } = useAuth();

  function choose(l: Locale) {
    setLocale(l);
    if (user && user.locale !== l) {
      api.updateMe({ locale: l }).then(setUser).catch(() => {});
    }
  }

  return (
    <div role="group" aria-label={t("language.label")} className={clsx("flex items-center gap-1.5", className)}>
      {LOCALES.map((l) => {
        const Flag = flags[l];
        return (
          <button
            key={l}
            type="button"
            onClick={() => choose(l)}
            aria-pressed={locale === l}
            title={l === "pt-BR" ? t("language.pt") : t("language.en")}
            className={clsx(
              "h-4 w-6 overflow-hidden border transition-opacity",
              locale === l ? "border-primary-text opacity-100" : "border-border opacity-50 hover:opacity-100",
            )}
          >
            <Flag />
          </button>
        );
      })}
    </div>
  );
}
