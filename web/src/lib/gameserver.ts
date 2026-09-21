import type { GameServer } from "./types";

export function serverTitle(server: GameServer): string {
  return server.spec.displayName || server.metadata.name;
}

export function restartRequired(server: GameServer): boolean {
  return server.status?.conditions?.some((c) => c.type === "RestartRequired" && c.status === "True") ?? false;
}
