import { type ReactNode, useEffect, useState } from "react";
import { Sidebar } from "./Sidebar";
import { CommandPalette } from "./CommandPalette";
import { useT } from "../../lib/i18n";
import { Wordmark } from "../ui/Wordmark";

export function AppShell({ children }: { children: ReactNode }) {
  const t = useT();
  const [drawer, setDrawer] = useState(false);
  const [palette, setPalette] = useState(false);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette((open) => !open);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const openPalette = () => {
    setDrawer(false);
    setPalette(true);
  };

  return (
    <div className="flex h-screen w-full flex-col bg-canvas md:flex-row">
      <div className="flex shrink-0 items-center justify-between border-b border-border bg-sidebar px-4 py-2.5 md:hidden">
        <Wordmark />
        <button aria-label={t("nav.openMenu")} onClick={() => setDrawer(true)} className="text-lg text-text-secondary">
          ☰
        </button>
      </div>

      <div className="hidden md:flex">
        <Sidebar onOpenPalette={openPalette} />
      </div>

      {drawer && (
        <div className="fixed inset-0 z-40 md:hidden">
          <div className="absolute inset-0 bg-black/60" onClick={() => setDrawer(false)} />
          <div className="absolute inset-y-0 left-0">
            <Sidebar onOpenPalette={openPalette} onNavigate={() => setDrawer(false)} />
          </div>
        </div>
      )}

      <main className="min-h-0 min-w-0 grow overflow-y-auto px-4 py-6 md:px-9 md:py-8">{children}</main>

      <CommandPalette open={palette} onClose={() => setPalette(false)} />
    </div>
  );
}
