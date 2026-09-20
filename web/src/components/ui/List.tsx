import type { HTMLAttributes } from "react";
import clsx from "clsx";

export function ListRow({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={clsx("group flex items-center justify-between gap-4 px-4 py-3 transition-colors hover:bg-surface-hover", className)}
      {...props}
    />
  );
}

export function RowActions({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={clsx(
        "flex items-center gap-2 md:opacity-0 md:transition-opacity md:group-focus-within:opacity-100 md:group-hover:opacity-100",
        className,
      )}
      {...props}
    />
  );
}

export function Badge({ className, ...props }: HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={clsx("rounded-sm bg-primary/15 px-2 py-0.5 font-sans text-[11px] font-semibold text-primary-text", className)}
      {...props}
    />
  );
}
