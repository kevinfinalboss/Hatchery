import clsx from "clsx";

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={clsx("font-display font-bold tracking-tight text-text-primary", className)}>
      hatchery<span className="text-primary-text">_</span>
    </span>
  );
}
