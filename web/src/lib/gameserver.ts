import type { GameServer, GameServerCrash } from "./types";

export function serverTitle(server: GameServer): string {
  return server.spec.displayName || server.metadata.name;
}

export function restartRequired(server: GameServer): boolean {
  return server.status?.conditions?.some((c) => c.type === "RestartRequired" && c.status === "True") ?? false;
}

// How long after a crash the server page keeps mentioning it, when the server came back on its own.
const RECENT_CRASH_MS = 10 * 60 * 1000;

export interface CrashNotice {
  crash: GameServerCrash;
  gaveUp: boolean;
  reason?: string;
}

/** What to tell the user about the server's last crash, or null when there is nothing worth showing. */
export function crashNotice(server: GameServer, now = Date.now()): CrashNotice | null {
  const crash = server.status?.lastCrash;
  if (!crash) return null;
  const cond = server.status?.conditions?.find((c) => c.type === "Crashed" && c.status === "True");
  if (!cond && now - new Date(crash.at).getTime() > RECENT_CRASH_MS) return null;
  return { crash, gaveUp: !!cond, reason: cond?.reason };
}
