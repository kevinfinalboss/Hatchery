import type {
  CreateGameServerRequest,
  EggList,
  FileEntry,
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
  if (options.body && !(options.body instanceof FormData) && !headers.has("Content-Type")) {
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
  // 204, or any bodyless response (several file endpoints answer 201 Created
  // with no body) — res.json() on an empty body throws a SyntaxError.
  if (res.status === 204 || res.headers.get("Content-Length") === "0") return undefined as T;
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

  listFiles: (ns: string, name: string, path = "/") =>
    request<FileEntry[]>(`/gameservers/${ns}/${name}/files?path=${encodeURIComponent(path)}`),

  // Raw text, not JSON — bypasses request()'s res.json() call. Only ever
  // called for files the UI has already decided are small text files (see
  // FileManager.tsx's isEditable).
  getFileContent: async (ns: string, name: string, path: string) => {
    const headers = new Headers();
    if (authToken) headers.set("Authorization", `Bearer ${authToken}`);
    const res = await fetch(
      `${API_BASE}/gameservers/${ns}/${name}/files/content?path=${encodeURIComponent(path)}`,
      { headers },
    );
    if (!res.ok) throw new ApiError(res.status, res.statusText);
    return res.text();
  },

  putFileContent: (ns: string, name: string, path: string, content: string) =>
    request<void>(`/gameservers/${ns}/${name}/files/content?path=${encodeURIComponent(path)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/octet-stream" },
      body: content,
    }),

  mkdir: (ns: string, name: string, path: string) =>
    request<void>(`/gameservers/${ns}/${name}/files/mkdir`, {
      method: "POST",
      body: JSON.stringify({ path }),
    }),

  renameFile: (ns: string, name: string, from: string, to: string) =>
    request<void>(`/gameservers/${ns}/${name}/files/rename`, {
      method: "POST",
      body: JSON.stringify({ from, to }),
    }),

  copyFile: (ns: string, name: string, from: string, to: string) =>
    request<void>(`/gameservers/${ns}/${name}/files/copy`, {
      method: "POST",
      body: JSON.stringify({ from, to }),
    }),

  deleteFiles: (ns: string, name: string, paths: string[]) =>
    request<void>(`/gameservers/${ns}/${name}/files/delete`, {
      method: "POST",
      body: JSON.stringify({ paths }),
    }),

  compressFiles: (ns: string, name: string, paths: string[], dest: string) =>
    request<void>(`/gameservers/${ns}/${name}/files/compress`, {
      method: "POST",
      body: JSON.stringify({ paths, dest }),
    }),

  decompressFile: (ns: string, name: string, path: string, dest: string) =>
    request<void>(`/gameservers/${ns}/${name}/files/decompress`, {
      method: "POST",
      body: JSON.stringify({ path, dest }),
    }),

  uploadFile: (ns: string, name: string, dirPath: string, file: File) => {
    const form = new FormData();
    form.append("file", file);
    return request<void>(`/gameservers/${ns}/${name}/files/upload?path=${encodeURIComponent(dirPath)}`, {
      method: "POST",
      body: form,
    });
  },

  // Fetches the blob with the Authorization header (a plain <a href> can't
  // carry one) and triggers a browser save via a synthetic link — same
  // reasoning as getFileContent bypassing request(), and it avoids putting
  // the session token in a URL the way consoleUrl below has to.
  downloadFiles: async (ns: string, name: string, paths: string[]) => {
    // Repeated params instead of one comma-joined value: a comma inside a
    // filename would survive the server's percent-decoding as a delimiter.
    const query = paths.map((p) => `paths=${encodeURIComponent(p)}`).join("&");
    const headers = new Headers();
    if (authToken) headers.set("Authorization", `Bearer ${authToken}`);
    const res = await fetch(`${API_BASE}/gameservers/${ns}/${name}/files/download?${query}`, { headers });
    if (!res.ok) throw new ApiError(res.status, res.statusText);
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
