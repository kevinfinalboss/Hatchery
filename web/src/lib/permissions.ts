import type { GameServer, Permission } from "./types";

export const ALL_PERMISSIONS: Permission[] = [
  "console.read",
  "console.write",
  "power",
  "files.read",
  "files.write",
  "backups.read",
  "backups.manage",
  "schedules",
];

export const PRESETS: Record<"readOnly" | "moderator" | "full", Permission[]> = {
  readOnly: ["console.read", "files.read", "backups.read"],
  moderator: ["console.read", "console.write", "power"],
  full: ALL_PERMISSIONS,
};

export function can(server: GameServer | undefined, perm: Permission): boolean {
  return !!server?.access?.permissions.includes(perm);
}
