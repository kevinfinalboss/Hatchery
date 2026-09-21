export interface ObjectMeta {
  name: string;
  namespace: string;
  uid?: string;
  creationTimestamp?: string;
  annotations?: Record<string, string>;
}

export type GameServerState = "Running" | "Stopped";

export type GameServerPhase =
  | ""
  | "Pending"
  | "Installing"
  | "Starting"
  | "Suspended"
  | "Running"
  | "Stopping"
  | "Stopped"
  | "Failed";

export interface GameServerVariable {
  name: string;
  value: string;
}

export interface GameServerSpec {
  displayName?: string;
  suspended?: boolean;
  suspendReason?: string;
  backupTarget?: BackupTarget;
  startCommand?: string;
  imageName?: string;
  eggRef: { name: string; scope?: EggScope };
  state: GameServerState;
  storage: { size: string; storageClassName?: string };
  variables?: GameServerVariable[];
  resources?: {
    requests?: Record<string, string>;
    limits?: Record<string, string>;
  };
}

export interface GameServerCondition {
  type: string;
  status: "True" | "False" | "Unknown";
  reason?: string;
  message?: string;
}

export interface GameServerStatus {
  phase: GameServerPhase;
  podName?: string;
  observedGeneration?: number;
  conditions?: GameServerCondition[];
}

export interface GameServer {
  metadata: ObjectMeta;
  spec: GameServerSpec;
  status?: GameServerStatus;
}

export interface GameServerList {
  items: GameServer[] | null;
}

export interface EggVariable {
  name: string;
  description?: string;
  default?: string;
  required?: boolean;
  userEditable?: boolean;
  validationRegex?: string;
}

export interface EggPort {
  name: string;
  containerPort: number;
  protocol?: string;
  default?: boolean;
}

export interface User {
  id: number;
  username: string;
  isAdmin: boolean;
}

export interface LoginResponse {
  token: string;
  expiresAt: string;
  user: User;
}

export interface SFTPSessionResponse {
  host: string;
  port: number;
  username: string;
  password: string;
  expiresAt: string;
  mode: "sidecar" | "maintenance";
}

export interface CreateGameServerRequest {
  name?: string;
  spec: Omit<GameServerSpec, "state"> & { state?: GameServerState };
}

export interface UpdateGameServerRequest {
  displayName?: string;
  backupTarget?: BackupTarget;
  startCommand?: string;
  imageName?: string;
  variables?: GameServerVariable[];
  resources?: { limits?: Record<string, string> };
}

export interface QuotaUsage {
  limit: OrgQuota;
  used: { cpu: string; memory: string; storage: string; gameServers: number };
}

export interface FileEntry {
  name: string;
  size: number;
  mode: string;
  isDir: boolean;
  modTime: string;
}

export type OrgRole = "owner" | "admin" | "member";

export interface OrgSummary {
  slug: string;
  name: string;
  role: OrgRole;
}

export interface BackupLimits {
  maxPerServer: number;
  maxPerOrg: number;
  retentionDays: number;
}

export interface OrgQuota {
  cpu: string;
  memory: string;
  storage: string;
  maxGameServers: number;
  backups?: BackupLimits;
}

export interface BackupTarget {
  connection: string;
  bucket?: string;
  prefix?: string;
}

export interface BackupRestore {
  name: string;
  phase: string;
  createdAt: string;
}

export interface BackupItem {
  name: string;
  createdAt: string;
  phase: string;
  destination: string;
  bucket?: string;
  prefix?: string;
  expiresAt?: string;
  completionTime?: string;
  deleting?: boolean;
  restore?: BackupRestore;
}

export interface BackupList {
  items: BackupItem[];
  usage: { server: number; org: number };
}

export interface BackupConnection {
  name: string;
  endpoint?: string;
  buckets: string[];
}

export interface BackupSettings {
  platform: { available: boolean; limits?: BackupLimits };
  connections: BackupConnection[];
}

export interface BackupConnectionRequest {
  endpoint: string;
  buckets: string[];
  accessKey?: string;
  secretKey?: string;
}

export interface OrgDetail extends OrgSummary {
  namespace: string;
  phase: string;
  ready: boolean;
  quota?: OrgQuota;
}

export interface Member {
  userId: number;
  username: string;
  role: OrgRole;
}

export type EggScope = "Catalog" | "Namespace";

export interface EggImage {
  name: string;
  image: string;
}

export interface EggSpec {
  images: EggImage[];
  startCommand: string;
  variables?: EggVariable[];
  ports?: EggPort[];
  recommendedResources?: { cpu?: string; memory?: string; disk?: string };
  [extra: string]: unknown;
}

export interface EggEntry {
  name: string;
  scope: EggScope;
  spec: EggSpec;
}

export interface AuditEvent {
  id: number;
  orgSlug?: string;
  actorUserId?: number;
  actorUsername: string;
  action: string;
  targetType?: string;
  targetName?: string;
  outcome: "success" | "denied" | "failed";
  ip?: string;
  metadata?: string;
  createdAt: string;
}

export interface AuditPage {
  events: AuditEvent[];
  nextBefore: number | null;
}

export interface ConsoleTicketResponse {
  ticket: string;
  expiresInSeconds: number;
}
