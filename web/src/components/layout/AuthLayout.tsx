import type { ReactNode } from "react";
import { LanguageSwitcher } from "../ui/LanguageSwitcher";
import { Wordmark } from "../ui/Wordmark";

export function AuthLayout({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <div className="flex min-h-screen w-full items-center justify-center bg-canvas px-4 py-10">
      <div className="fixed right-4 top-4">
        <LanguageSwitcher />
      </div>
      <div className="flex w-[380px] max-w-full flex-col gap-5 border border-border bg-surface p-8">
        <div className="text-center text-2xl">
          <Wordmark />
        </div>
        {title && <div className="font-display text-lg font-semibold text-text-primary">{title}</div>}
        {children}
      </div>
    </div>
  );
}

export function FormError({ message }: { message: string | null }) {
  return message ? <div className="font-sans text-sm text-status-failed">{message}</div> : null;
}

export function FormNote({ children }: { children: ReactNode }) {
  return <div className="font-sans text-sm text-text-secondary">{children}</div>;
}
