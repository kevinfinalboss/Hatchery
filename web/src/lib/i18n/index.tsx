import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { type Dictionary, ptBR } from "./pt-BR";
import { en } from "./en";

export type Locale = "pt-BR" | "en";
export const LOCALES: readonly Locale[] = ["pt-BR", "en"];

const dictionaries: Record<Locale, Dictionary> = { "pt-BR": ptBR, en };
const STORAGE_KEY = "hatchery_locale";

type Leaves<T, P extends string = ""> = {
  [K in keyof T & string]: T[K] extends string ? `${P}${K}` : Leaves<T[K], `${P}${K}.`>;
}[keyof T & string];

export type TKey = Leaves<Dictionary>;
type PluralBase<K extends string> = K extends `${infer B}_one` ? B : never;
export type PluralKey = PluralBase<TKey>;

type Vars = Record<string, string | number>;

function lookup(dict: Dictionary, key: string): string | undefined {
  let cur: unknown = dict;
  for (const part of key.split(".")) {
    if (typeof cur !== "object" || cur === null) return undefined;
    cur = (cur as Record<string, unknown>)[part];
  }
  return typeof cur === "string" ? cur : undefined;
}

function interpolate(text: string, vars?: Vars): string {
  if (!vars) return text;
  return text.replace(/\{(\w+)\}/g, (match, name: string) => (name in vars ? String(vars[name]) : match));
}

function readStored(): Locale | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v === "pt-BR" || v === "en" ? v : null;
  } catch {
    return null;
  }
}

function detect(): Locale {
  return readStored() ?? (navigator.language?.toLowerCase().startsWith("pt") ? "pt-BR" : "en");
}

interface I18nContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: TKey, vars?: Vars) => string;
  plural: (base: PluralKey, count: number, vars?: Vars) => string;
  formatDateTime: (value: string | number | Date) => string;
  timeZone: string;
  setTimeZone: (tz: string) => void;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(detect);
  const [timeZone, setTimeZone] = useState<string>("");

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
    }
  }, []);

  const value = useMemo<I18nContextValue>(() => {
    const dict = dictionaries[locale];
    const t = (key: TKey, vars?: Vars) => interpolate(lookup(dict, key) ?? lookup(ptBR, key) ?? key, vars);
    const rules = new Intl.PluralRules(locale);
    const plural = (base: PluralKey, count: number, vars?: Vars) => {
      const form = rules.select(count) === "one" ? "one" : "other";
      return t(`${base}_${form}` as TKey, { count, ...vars });
    };
    const formatDateTime = (v: string | number | Date) =>
      new Date(v).toLocaleString(locale, timeZone ? { timeZone } : undefined);
    return { locale, setLocale, t, plural, formatDateTime, timeZone, setTimeZone };
  }, [locale, setLocale, timeZone]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used within an I18nProvider");
  return ctx;
}

export function useT() {
  return useI18n().t;
}
