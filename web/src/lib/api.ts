import type {
  AuditPage,
  BackupItem,
  BackupList,
  BackupRestore,
  BackupSettings,
  ConsoleTicketResponse,
  BackupConnectionRequest,
  CreateGameServerRequest,
  EggEntry,
  EggSpec,
  FileEntry,
  GameServer,
  GameServerList,
  GameServerState,
  ImagePolicy,
  LoginResponse,
  Member,
  OrgDetail,
  OrgQuota,
  OrgRole,
  OrgSummary,
  QuotaUsage,
  ScheduleItem,
  ScheduleWrite,
  ServerGrant,
  SFTPSessionResponse,
  UpdateGameServerRequest,
  User,
} from "./types";
import type { MetricsRange, MetricsResponse, RuntimeResponse } from "./metrics";

const API_BASE = "/api/v1";
const TOKEN_STORAGE_KEY = "hatchery_token";

export class ApiError extends Error {
  status: number;
  retryAfter?: number;
  constructor(status: number, message: string, retryAfter?: number) {
    super(message);
    this.status = status;
    this.retryAfter = retryAfter;
  }
}

function readStoredToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_STORAGE_KEY);
  } catch {
    return null;
  }
}

let authToken: string | null = readStoredToken();

export function setAuthToken(token: string | null) {
  authToken = token;
  try {
    if (token) localStorage.setItem(TOKEN_STORAGE_KEY, token);
    else localStorage.removeItem(TOKEN_STORAGE_KEY);
  } catch {
  }
}

export function getAuthToken() {
  return authToken;
}

async function failure(res: Response): Promise<ApiError> {
  let message = res.statusText;
  try {
    const body = (await res.json()) as { error?: string };
    if (body?.error) message = body.error;
  } catch {
  }
  const retry = Number(res.headers.get("Retry-After"));
  return new ApiError(res.status, message, Number.isFinite(retry) && retry > 0 ? retry : undefined);
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  if (authToken) headers.set("Authorization", `Bearer ${authToken}`);
  if (options.body && !(options.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const res = await fetch(`${API_BASE}${path}`, { ...options, headers });
  if (!res.ok) throw await failure(res);
  // 204, or any bodyless response (several file endpoints answer 201 Created
  // with no body) — res.json() on an empty body throws a SyntaxError.
  if (res.status === 204 || res.headers.get("Content-Length") === "0") return undefined as T;
  return (await res.json()) as T;
}

const gs = (org: string, name: string) => `/orgs/${org}/gameservers/${name}`;

export const api = {
  login: (username: string, password: string) =>
    request<LoginResponse>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<void>("/auth/logout", { method: "POST" }),
  me: () => request<User>("/auth/me"),

  // Organizations
  listOrgs: () => request<OrgSummary[]>("/orgs"),
  getOrg: (org: string) => request<OrgDetail>(`/orgs/${org}`),
  createOrg: (body: { slug: string; name: string; ownerUsername: string; quota: OrgQuota }) =>
    request<OrgSummary>("/orgs", { method: "POST", body: JSON.stringify(body) }),
  updateOrgQuota: (org: string, quota: OrgQuota) =>
    request<OrgQuota>(`/orgs/${org}/quota`, { method: "PATCH", body: JSON.stringify(quota) }),
  deleteOrg: (org: string) => request<void>(`/orgs/${org}`, { method: "DELETE" }),

  // Members
  listMembers: (org: string) => request<Member[]>(`/orgs/${org}/members`),
  addMember: (org: string, username: string, role: OrgRole) =>
    request<Member>(`/orgs/${org}/members`, { method: "POST", body: JSON.stringify({ username, role }) }),
  setMemberRole: (org: string, userId: number, role: OrgRole) =>
    request<{ role: OrgRole; hasServerAccess: boolean }>(`/orgs/${org}/members/${userId}`, {
      method: "PATCH",
      body: JSON.stringify({ role }),
    }),
  removeMember: (org: string, userId: number) =>
    request<void>(`/orgs/${org}/members/${userId}`, { method: "DELETE" }),
  getMemberPermissions: (org: string, userId: number) =>
    request<{ grants: ServerGrant[] }>(`/orgs/${org}/members/${userId}/permissions`),
  putMemberPermissions: (org: string, userId: number, grants: ServerGrant[]) =>
    request<{ grants: ServerGrant[] }>(`/orgs/${org}/members/${userId}/permissions`, {
      method: "PUT",
      body: JSON.stringify({ grants }),
    }),

  // Eggs
  listOrgEggs: (org: string) => request<EggEntry[]>(`/orgs/${org}/eggs`),
  createOrgEgg: (org: string, name: string, spec: EggSpec) =>
    request<EggEntry>(`/orgs/${org}/eggs`, { method: "POST", body: JSON.stringify({ name, spec }) }),
  updateOrgEgg: (org: string, name: string, spec: EggSpec) =>
    request<EggEntry>(`/orgs/${org}/eggs/${name}`, { method: "PUT", body: JSON.stringify({ spec }) }),
  deleteOrgEgg: (org: string, name: string) => request<void>(`/orgs/${org}/eggs/${name}`, { method: "DELETE" }),
  listCatalogEggs: () => request<EggEntry[]>("/catalog/eggs"),
  createCatalogEgg: (name: string, spec: EggSpec) =>
    request<EggEntry>("/catalog/eggs", { method: "POST", body: JSON.stringify({ name, spec }) }),
  updateCatalogEgg: (name: string, spec: EggSpec) =>
    request<EggEntry>(`/catalog/eggs/${name}`, { method: "PUT", body: JSON.stringify({ spec }) }),
  deleteCatalogEgg: (name: string) => request<void>(`/catalog/eggs/${name}`, { method: "DELETE" }),

  // Audit
  listOrgAudit: (org: string, before?: number) =>
    request<AuditPage>(`/orgs/${org}/audit${before ? `?before=${before}` : ""}`),
  listPlatformAudit: (before?: number) => request<AuditPage>(`/audit${before ? `?before=${before}` : ""}`),

  // GameServers
  listGameServers: (org: string) => request<GameServerList>(`/orgs/${org}/gameservers`),
  getGameServer: (org: string, name: string) => request<GameServer>(gs(org, name)),
  createGameServer: (org: string, body: CreateGameServerRequest) =>
    request<GameServer>(`/orgs/${org}/gameservers`, { method: "POST", body: JSON.stringify(body) }),
  updateGameServer: (org: string, name: string, body: UpdateGameServerRequest) =>
    request<GameServer>(gs(org, name), { method: "PATCH", body: JSON.stringify(body) }),
  getQuota: (org: string) => request<QuotaUsage>(`/orgs/${org}/quota`),
  getImagePolicy: (org: string) => request<ImagePolicy>(`/orgs/${org}/image-policy`),
  deleteGameServer: (org: string, name: string) => request<void>(gs(org, name), { method: "DELETE" }),
  setGameServerState: (org: string, name: string, state: GameServerState) =>
    request<GameServer>(`${gs(org, name)}/state`, { method: "PATCH", body: JSON.stringify({ state }) }),
  sftpSession: (org: string, name: string) =>
    request<SFTPSessionResponse>(`${gs(org, name)}/sftp-session`, { method: "POST" }),

  listFiles: (org: string, name: string, path = "/") =>
    request<FileEntry[]>(`${gs(org, name)}/files?path=${encodeURIComponent(path)}`),

  // Raw text, not JSON — bypasses request()'s res.json() call. Only ever
  // called for files the UI has already decided are small text files (see
  // FileManager.tsx's isEditable).
  getFileContent: async (org: string, name: string, path: string) => {
    const headers = new Headers();
    if (authToken) headers.set("Authorization", `Bearer ${authToken}`);
    const res = await fetch(`${API_BASE}${gs(org, name)}/files/content?path=${encodeURIComponent(path)}`, { headers });
    if (!res.ok) throw await failure(res);
    return res.text();
  },

  putFileContent: (org: string, name: string, path: string, content: string) =>
    request<void>(`${gs(org, name)}/files/content?path=${encodeURIComponent(path)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/octet-stream" },
      body: content,
    }),

  mkdir: (org: string, name: string, path: string) =>
    request<void>(`${gs(org, name)}/files/mkdir`, { method: "POST", body: JSON.stringify({ path }) }),

  renameFile: (org: string, name: string, from: string, to: string) =>
    request<void>(`${gs(org, name)}/files/rename`, { method: "POST", body: JSON.stringify({ from, to }) }),

  copyFile: (org: string, name: string, from: string, to: string) =>
    request<void>(`${gs(org, name)}/files/copy`, { method: "POST", body: JSON.stringify({ from, to }) }),

  deleteFiles: (org: string, name: string, paths: string[]) =>
    request<void>(`${gs(org, name)}/files/delete`, { method: "POST", body: JSON.stringify({ paths }) }),

  compressFiles: (org: string, name: string, paths: string[], dest: string) =>
    request<void>(`${gs(org, name)}/files/compress`, { method: "POST", body: JSON.stringify({ paths, dest }) }),

  decompressFile: (org: string, name: string, path: string, dest: string) =>
    request<void>(`${gs(org, name)}/files/decompress`, { method: "POST", body: JSON.stringify({ path, dest }) }),

  uploadFile: (org: string, name: string, dirPath: string, file: File) => {
    const form = new FormData();
    form.append("file", file);
    return request<void>(`${gs(org, name)}/files/upload?path=${encodeURIComponent(dirPath)}`, {
      method: "POST",
      body: form,
    });
  },

  // Fetches the blob with the Authorization header (a plain <a href> can't
  // carry one) and triggers a browser save via a synthetic link — it avoids
  // putting the session token in a URL.
  downloadFiles: async (org: string, name: string, paths: string[]) => {
    // Repeated params instead of one comma-joined value: a comma inside a
    // filename would survive the server's percent-decoding as a delimiter.
    const query = paths.map((p) => `paths=${encodeURIComponent(p)}`).join("&");
    const headers = new Headers();
    if (authToken) headers.set("Authorization", `Bearer ${authToken}`);
    const res = await fetch(`${API_BASE}${gs(org, name)}/files/download?${query}`, { headers });
    if (!res.ok) throw await failure(res);
    const blob = await res.blob();
    const disposition = res.headers.get("Content-Disposition") ?? "";
    const match = /filename="?([^"]+)"?/.exec(disposition);
    const fallback = paths.length > 1 ? "download.zip" : (paths[0].split("/").pop() ?? "download");
    const filename = match?.[1] ?? fallback;
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  },

  listUsers: () => request<User[]>("/users"),
  createUser: (body: { username: string; password: string; isAdmin: boolean }) =>
    request<User>("/users", { method: "POST", body: JSON.stringify(body) }),
  deleteUser: (id: number) => request<void>(`/users/${id}`, { method: "DELETE" }),

  getBackupSettings: (org: string) => request<BackupSettings>(`/orgs/${org}/backup-settings`),
  putBackupConnection: (org: string, name: string, body: BackupConnectionRequest) =>
    request<BackupSettings>(`/orgs/${org}/backup-connections/${name}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteBackupConnection: (org: string, name: string) =>
    request<void>(`/orgs/${org}/backup-connections/${name}`, { method: "DELETE" }),
  listBackups: (org: string, name: string) => request<BackupList>(`${gs(org, name)}/backups`),
  createBackup: (org: string, name: string) => request<BackupItem>(`${gs(org, name)}/backups`, { method: "POST" }),
  deleteBackup: (org: string, name: string, backup: string) =>
    request<void>(`${gs(org, name)}/backups/${backup}`, { method: "DELETE" }),
  restoreBackup: (org: string, name: string, backup: string) =>
    request<BackupRestore>(`${gs(org, name)}/backups/${backup}/restore`, { method: "POST" }),
  suspendGameServer: (org: string, name: string, reason: string) =>
    request<GameServer>(`${gs(org, name)}/suspend`, { method: "POST", body: JSON.stringify({ reason }) }),
  unsuspendGameServer: (org: string, name: string) =>
    request<GameServer>(`${gs(org, name)}/unsuspend`, { method: "POST" }),
  reinstallGameServer: (org: string, name: string) =>
    request<GameServer>(`${gs(org, name)}/reinstall`, { method: "POST" }),
  restartGameServer: (org: string, name: string) =>
    request<GameServer>(`${gs(org, name)}/restart`, { method: "POST" }),

  getMetrics: (org: string, name: string, range: MetricsRange) =>
    request<MetricsResponse>(`${gs(org, name)}/metrics?range=${range}`),

  getRuntime: (org: string, name: string) => request<RuntimeResponse>(`${gs(org, name)}/runtime`),

  logsPath: (org: string, name: string, tailLines?: number, container?: "install" | "configure") =>
    `${API_BASE}${gs(org, name)}/logs?${tailLines ? `tailLines=${tailLines}&` : ""}${container ? `container=${container}&` : ""}follow=true`,
  
  consoleTicket: (org: string, name: string) =>
    request<ConsoleTicketResponse>(`${gs(org, name)}/console-ticket`, { method: "POST" }),

  consoleUrl: (org: string, name: string, ticket: string) => {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    return `${proto}://${location.host}${API_BASE}${gs(org, name)}/console?ticket=${encodeURIComponent(ticket)}`;
  },

  listSchedules: (org: string, name: string) => request<ScheduleItem[]>(`${gs(org, name)}/schedules`),
  createSchedule: (org: string, name: string, body: ScheduleWrite) =>
    request<ScheduleItem>(`${gs(org, name)}/schedules`, { method: "POST", body: JSON.stringify(body) }),
  updateSchedule: (org: string, name: string, schedule: string, body: ScheduleWrite) =>
    request<ScheduleItem>(`${gs(org, name)}/schedules/${schedule}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteSchedule: (org: string, name: string, schedule: string) =>
    request<void>(`${gs(org, name)}/schedules/${schedule}`, { method: "DELETE" }),
  runSchedule: (org: string, name: string, schedule: string) =>
    request<void>(`${gs(org, name)}/schedules/${schedule}/run`, { method: "POST" }),
};
