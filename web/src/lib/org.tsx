import { createContext, useContext, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import { useAuth } from "./auth";
import type { OrgRole, OrgSummary } from "./types";

const ORG_STORAGE_KEY = "hatchery_org";

const rank: Record<OrgRole, number> = { member: 1, admin: 2, owner: 3 };

export function atLeast(role: OrgRole | undefined, min: OrgRole): boolean {
  return role !== undefined && rank[role] >= rank[min];
}

export function roleOf(orgs: OrgSummary[], slug: string): OrgRole | undefined {
  return orgs.find((o) => o.slug === slug)?.role;
}

function readStoredOrg(): string | null {
  try {
    return localStorage.getItem(ORG_STORAGE_KEY);
  } catch {
    return null;
  }
}

interface OrgContextValue {
  orgs: OrgSummary[];
  current: OrgSummary | null;
  setCurrent: (slug: string) => void;
  loading: boolean;
}

const OrgContext = createContext<OrgContextValue | null>(null);

export function OrgProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const [slug, setSlug] = useState<string | null>(readStoredOrg);
  const { data, isLoading } = useQuery({
    queryKey: ["orgs"],
    queryFn: api.listOrgs,
    enabled: !!user,
  });

  const orgs = data ?? [];
  const current = orgs.find((o) => o.slug === slug) ?? orgs[0] ?? null;

  function setCurrent(next: string) {
    setSlug(next);
    try {
      localStorage.setItem(ORG_STORAGE_KEY, next);
    } catch {
    }
  }

  return (
    <OrgContext.Provider value={{ orgs, current, setCurrent, loading: !!user && isLoading }}>
      {children}
    </OrgContext.Provider>
  );
}

export function useOrg() {
  const ctx = useContext(OrgContext);
  if (!ctx) throw new Error("useOrg must be used within an OrgProvider");
  return ctx;
}
