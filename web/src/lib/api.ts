import type {
  CreateGameServerRequest,
  EggList,
  GameServer,
  GameServerList,
  GameServerRef,
  GameServerState,
  LoginResponse,
  SFTPSessionResponse,
  User,
} from "./types";

const API_BASE = "/api/v1";
const TOKEN_STORAGE_KEY = "hatchery_token";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

let authToken: string | null = localStorage.getItem(TOKEN_STORAGE_KEY);

export function setAuthToken(token: string | null) {
  authToken = token;
  if (token) localStorage.setItem(TOKEN_STORAGE_KEY, token);
  else localStorage.removeItem(TOKEN_STORAGE_KEY);
}

export function getAuthToken() {
  return authToken;
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  if (authToken) headers.set("Authorization", `Bearer ${authToken}`);
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const res = await fetch(`${API_BASE}${path}`, { ...options, headers });
  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = (await res.json()) as { error?: string };
      if (body?.error) message = body.error;
    } catch {
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  login: (username: string, password: string) =>
    request<LoginResponse>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<void>("/auth/logout", { method: "POST" }),
  me: () => request<User>("/auth/me"),

  listGameServers: () => request<GameServerList>("/gameservers"),
  getGameServer: (ns: string, name: string) => request<GameServer>(`/gameservers/${ns}/${name}`),
  createGameServer: (body: CreateGameServerRequest) =>
    request<GameServer>("/gameservers", { method: "POST", body: JSON.stringify(body) }),
  deleteGameServer: (ns: string, name: string) =>
    request<void>(`/gameservers/${ns}/${name}`, { method: "DELETE" }),
  setGameServerState: (ns: string, name: string, state: GameServerState) =>
    request<GameServer>(`/gameservers/${ns}/${name}/state`, {
      method: "PATCH",
      body: JSON.stringify({ state }),
    }),
  sftpSession: (ns: string, name: string) =>
    request<SFTPSessionResponse>(`/gameservers/${ns}/${name}/sftp-session`, { method: "POST" }),

  listEggs: () => request<EggList>("/eggs"),

  listUsers: () => request<User[]>("/users"),
  createUser: (body: { username: string; password: string; isAdmin: boolean }) =>
    request<User>("/users", { method: "POST", body: JSON.stringify(body) }),
  deleteUser: (id: number) => request<void>(`/users/${id}`, { method: "DELETE" }),
  listUserPermissions: (id: number) => request<GameServerRef[] | null>(`/users/${id}/permissions`),
  grantPermission: (id: number, ref: GameServerRef) =>
    request<GameServerRef>(`/users/${id}/permissions`, {
      method: "POST",
      body: JSON.stringify(ref),
    }),
  revokePermission: (id: number, ref: GameServerRef) =>
    request<void>(`/users/${id}/permissions/${ref.namespace}/${ref.name}`, { method: "DELETE" }),

  logsPath: (ns: string, name: string, tailLines = 200) =>
    `${API_BASE}/gameservers/${ns}/${name}/logs?tailLines=${tailLines}&follow=true`,

  consoleUrl: (ns: string, name: string) => {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const token = encodeURIComponent(authToken ?? "");
    return `${proto}://${location.host}${API_BASE}/gameservers/${ns}/${name}/console?token=${token}`;
  },
};
