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
  | "Failed"
  | "Crashed";

export interface GameServerPublicExposurePort {
  name: string;
  port: number;
}

export interface GameServerPublicExposureStatus {
  host?: string;
  ports?: GameServerPublicExposurePort[];
}

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
  publicExposure?: { enabled?: boolean };
  autoRestart?: boolean;
}

export interface GameServerCrash {
  at: string;
  exitCode: number;
  reason?: string;
  oomKilled?: boolean;
}

export interface CrashLog {
  at: string;
  exitCode: number;
  reason?: string;
  oomKilled: boolean;
  log: string;
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
  publicExposure?: GameServerPublicExposureStatus;
  recentCrashes?: string[];
  lastCrash?: GameServerCrash;
}

export type Permission =
  | "console.read"
  | "console.write"
  | "power"
  | "files.read"
  | "files.write"
  | "backups.read"
  | "backups.manage"
  | "schedules";

export interface ServerGrant {
  gameserver: string; // server name or "*"
  permissions: Permission[];
}

export interface GameServerAccess {
  permissions: Permission[];
}

export interface GameServer {
  metadata: ObjectMeta;
  spec: GameServerSpec;
  status?: GameServerStatus;
  access?: GameServerAccess;
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

export type ProfileLocale = "" | "pt-BR" | "en";

export interface Profile {
  displayName: string;
  locale: ProfileLocale;
  timeZone: string;
  discord: string;
  minecraftUsername: string;
  steamId: string;
}

export interface User extends Profile {
  id: number;
  username: string;
  email: string;
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
  publicExposureEnabled?: boolean;
  autoRestart?: boolean;
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
  inviteUrl?: string;
  inviteError?: string;
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
  extraImageRegistries?: string[];
}

export interface ImagePolicy {
  enforced: boolean;
  registries: string[];
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
  displayName: string;
  discord: string;
  minecraftUsername: string;
  steamId: string;
  email?: string;
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

export type ScheduleAction = "Command" | "Restart" | "Start" | "Stop" | "Backup";

export interface ScheduleTask {
  action: ScheduleAction;
  command?: string;
  delaySeconds?: number;
  keepLast?: number;
}

export interface ScheduleSpec {
  gameServerRef: { name: string };
  displayName?: string;
  cron: string;
  timeZone?: string;
  suspend?: boolean;
  onlyWhenRunning: boolean;
  tasks: ScheduleTask[];
}

export type ScheduleRunResult = "Succeeded" | "Failed" | "Skipped";

export interface ScheduleRun {
  startedAt: string;
  finishedAt?: string;
  result: ScheduleRunResult;
  message?: string;
}

export interface ActiveScheduleRun {
  id: string;
  startedAt: string;
  taskIndex: number;
  nextTaskAt: string;
}

export interface ScheduleStatus {
  nextScheduleTime?: string;
  lastScheduleTime?: string;
  lastRun?: ScheduleRun;
  activeRun?: ActiveScheduleRun;
  lastRunNow?: string;
  conditions?: GameServerCondition[];
}

export interface ScheduleItem {
  name: string;
  spec: ScheduleSpec;
  status: ScheduleStatus;
}

export interface ScheduleWrite {
  displayName: string;
  cron: string;
  timeZone: string;
  suspend: boolean;
  onlyWhenRunning?: boolean;
  tasks: ScheduleTask[];
}

export interface Invitation {
  id: number;
  email: string;
  role: OrgRole;
  invitedBy: string;
  createdAt: string;
  expiresAt: string;
  expired: boolean;
}

export interface InviteResponse {
  invitation: Invitation;
  inviteUrl?: string;
}

export interface InvitationPreview {
  orgName: string;
  orgSlug: string;
  role: OrgRole;
  email: string;
  invitedBy: string;
  accountExists: boolean;
}

export interface AuthFeatures {
  passwordReset: boolean;
}
