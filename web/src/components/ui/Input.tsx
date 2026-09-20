import { type InputHTMLAttributes, type ReactNode, forwardRef } from "react";
import clsx from "clsx";

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={clsx(
        "rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary",
        "placeholder:text-text-tertiary focus:outline-none focus:ring-2 focus:ring-primary",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";

export function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={htmlFor} className="font-sans text-xs font-medium text-text-secondary">
        {label}
      </label>
      {children}
    </div>
  );
}
