import { request } from "./api";

export type ModKind = "plugin" | "mod";

export interface ModsContext {
  kind: ModKind;
  loaders: string[];
  directory: string;
  gameVersion: string | null;
  sources: string[];
}

export interface ModSearchResult {
  source: string;
  projectId: string;
  slug: string;
  title: string;
  description: string;
  iconUrl: string;
  author: string;
  pageUrl: string;
  downloads: number;
  distributionBlocked: boolean;
}

export interface ModSearchPage {
  results: ModSearchResult[];
  total: number;
}

export interface ModVersion {
  source: string;
  id: string;
  projectId: string;
  name: string;
  versionNumber: string;
  gameVersions: string[] | null;
  loaders: string[] | null;
  file: { filename: string; size: number };
  publishedAt: string;
  distributionBlocked: boolean;
  pageUrl: string;
}

export interface InstalledMod {
  file: string;
  size: number;
  source?: string;
  projectId?: string;
  title?: string;
  iconUrl?: string;
  pageUrl?: string;
  version?: string;
  latestVersion?: string;
  latestVersionId?: string;
  updateAvailable: boolean;
}

export interface InstalledList {
  items: InstalledMod[];
  warnings: string[];
}

export interface InstallResult {
  installed: { projectId: string; versionNumber: string; file: string }[];
  skipped: string[];
  warnings?: string[];
}

const base = (org: string, name: string) => `/orgs/${org}/gameservers/${name}/mods`;

function qs(params: Record<string, string | number | undefined>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") p.set(k, String(v));
  const s = p.toString();
  return s ? `?${s}` : "";
}

export const modsApi = {
  context: (org: string, name: string) => request<ModsContext>(`${base(org, name)}/context`),
  search: (org: string, name: string, q: { source: string; q: string; gameVersion?: string; page?: number }) =>
    request<ModSearchPage>(`${base(org, name)}/search${qs(q)}`),
  versions: (org: string, name: string, source: string, projectId: string, gameVersion?: string) =>
    request<ModVersion[]>(`${base(org, name)}/projects/${source}/${encodeURIComponent(projectId)}/versions${qs({ gameVersion })}`),
  install: (org: string, name: string, body: { source: string; projectId: string; versionId?: string; gameVersion?: string }) =>
    request<InstallResult>(`${base(org, name)}/install`, { method: "POST", body: JSON.stringify(body) }),
  installed: (org: string, name: string, gameVersion?: string) =>
    request<InstalledList>(`${base(org, name)}/installed${qs({ gameVersion })}`),
  update: (org: string, name: string, file: string) =>
    request<InstallResult["installed"][number]>(`${base(org, name)}/update`, { method: "POST", body: JSON.stringify({ file }) }),
  remove: (org: string, name: string, file: string) =>
    request<void>(`${base(org, name)}/installed${qs({ file })}`, { method: "DELETE" }),
};
