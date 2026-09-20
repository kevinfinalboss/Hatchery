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
  | "Running"
  | "Stopping"
  | "Stopped"
  | "Failed";

export interface GameServerVariable {
  name: string;
  value: string;
}

export interface GameServerSpec {
  eggRef: { name: string };
  state: GameServerState;
  storage: { size: string; storageClassName?: string };
  variables?: GameServerVariable[];
  resources?: {
    requests?: Record<string, string>;
    limits?: Record<string, string>;
  };
}

export interface GameServerStatus {
  phase: GameServerPhase;
  podName?: string;
  observedGeneration?: number;
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
}

export interface EggPort {
  name: string;
  containerPort: number;
  protocol?: string;
  default?: boolean;
}

export interface Egg {
  metadata: ObjectMeta;
  spec: {
    image: string;
    startCommand: string;
    variables?: EggVariable[];
    ports?: EggPort[];
  };
}

export interface EggList {
  items: Egg[] | null;
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

export interface GameServerRef {
  namespace: string;
  name: string;
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
  name: string;
  namespace: string;
  spec: GameServerSpec;
}

export interface FileEntry {
  name: string;
  size: number;
  mode: string;
  isDir: boolean;
  modTime: string;
}
