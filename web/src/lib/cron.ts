export type Preset =
  | { kind: "daily"; hour: number; minute: number }
  | { kind: "everyHours"; hours: 1 | 2 | 3 | 4 | 6 | 8 | 12 }
  | { kind: "weekly"; weekday: number; hour: number; minute: number } // 0 = Sunday
  | { kind: "custom"; cron: string };

export function presetToCron(p: Preset): string {
  switch (p.kind) {
    case "daily":
      return `${p.minute} ${p.hour} * * *`;
    case "everyHours":
      return `0 */${p.hours} * * *`;
    case "weekly":
      return `${p.minute} ${p.hour} * * ${p.weekday}`;
    case "custom":
      return p.cron.trim();
  }
}

// cronToPreset recognises exactly what presetToCron produces; anything else is "custom".
export function cronToPreset(cron: string): Preset {
  const f = cron.trim().split(/\s+/);
  if (f.length === 5) {
    const [mi, h, dom, mon, dow] = f;
    const num = (s: string) => (/^\d+$/.test(s) ? Number(s) : NaN);
    if (dom === "*" && mon === "*") {
      if (dow === "*" && !isNaN(num(mi)) && !isNaN(num(h))) return { kind: "daily", hour: num(h), minute: num(mi) };
      if (dow !== "*" && !isNaN(num(dow)) && !isNaN(num(mi)) && !isNaN(num(h)))
        return { kind: "weekly", weekday: num(dow), hour: num(h), minute: num(mi) };
      const every = /^\*\/(\d+)$/.exec(h);
      if (mi === "0" && every && dow === "*" && [1, 2, 3, 4, 6, 8, 12].includes(Number(every[1])))
        return { kind: "everyHours", hours: Number(every[1]) as 1 | 2 | 3 | 4 | 6 | 8 | 12 };
    }
  }
  return { kind: "custom", cron };
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

// timeOf formats a preset's hour/minute as "HH:MM" for interpolation into a translated string.
export function timeOf(p: { hour: number; minute: number }): string {
  return `${pad2(p.hour)}:${pad2(p.minute)}`;
}

// weekdayName returns the localized long weekday name (Intl handles this; 2026-01-04 is a Sunday,
// used as a stable anchor so weekday math never depends on "today").
export function weekdayName(weekday: number, locale: string): string {
  const anchor = new Date(Date.UTC(2026, 0, 4 + weekday));
  return new Intl.DateTimeFormat(locale, { weekday: "long", timeZone: "UTC" }).format(anchor);
}
