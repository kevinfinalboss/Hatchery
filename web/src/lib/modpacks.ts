import { request } from "./api";
import type { ModSearchPage, ModVersion } from "./mods";

export interface ModpackVersion extends ModVersion {
  gameVersion: string;
  imageName: string;
}

export interface ModpackStatus {
  source: string;
  projectId: string;
  version: ModpackVersion | null;
  latest: ModpackVersion | null;
  updateAvailable: boolean;
}

export const MODPACK_VARS = {
  source: "HATCHERY_PACK_SOURCE",
  pack: "HATCHERY_PACK",
  projectId: "HATCHERY_PACK_PROJECT_ID",
  version: "HATCHERY_PACK_VERSION",
} as const;

export const modpacksApi = {
  search: (org: string, source: string, q: string, page = 0) =>
    request<ModSearchPage>(`/orgs/${org}/modpacks/search?${new URLSearchParams({ source, q, page: String(page) })}`),
  versions: (org: string, source: string, projectId: string) =>
    request<ModpackVersion[]>(`/orgs/${org}/modpacks/${source}/${encodeURIComponent(projectId)}/versions`),
  status: (org: string, name: string) => request<ModpackStatus>(`/orgs/${org}/gameservers/${name}/modpack`),
  update: (org: string, name: string, versionId: string) =>
    request<ModpackVersion>(`/orgs/${org}/gameservers/${name}/modpack/update`, { method: "POST", body: JSON.stringify({ versionId }) }),
};
