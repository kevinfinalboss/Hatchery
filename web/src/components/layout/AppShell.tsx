import type { ReactNode } from "react";
import { Sidebar } from "./Sidebar";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-screen w-full bg-canvas">
      <Sidebar />
      <main className="min-w-0 grow overflow-y-auto px-9 py-8">{children}</main>
    </div>
  );
}
