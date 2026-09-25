import clsx from "clsx";

const COLORS = ["#b91c1c", "#b45309", "#4d7c0f", "#047857", "#0e7490", "#1d4ed8", "#7e22ce", "#be185d"];

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "";
  const first = [...parts[0]][0] ?? "";
  const last = parts.length > 1 ? ([...parts[parts.length - 1]][0] ?? "") : "";
  return (first + last).toUpperCase();
}

function colorFor(key: string): string {
  let h = 0;
  for (const ch of key) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
  return COLORS[h % COLORS.length];
}

const sizes = { sm: "h-6 w-6 text-[10px]", md: "h-8 w-8 text-xs", lg: "h-16 w-16 text-xl" } as const;

export function Avatar({
  user,
  size = "md",
  className,
}: {
  user: { username: string; displayName?: string };
  size?: keyof typeof sizes;
  className?: string;
}) {
  const label = user.displayName?.trim() || user.username;
  return (
    <span
      aria-hidden
      className={clsx("inline-flex shrink-0 select-none items-center justify-center rounded-full font-sans font-semibold text-white", sizes[size], className)}
      style={{ backgroundColor: colorFor(user.username) }}
    >
      {initials(label)}
    </span>
  );
}
