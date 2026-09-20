export type MetricsRange = "15m" | "1h" | "6h";
export const METRICS_RANGES: readonly MetricsRange[] = ["15m", "1h", "6h"];
export const RANGE_MS: Record<MetricsRange, number> = {
  "15m": 15 * 60_000,
  "1h": 60 * 60_000,
  "6h": 6 * 60 * 60_000,
};

export interface MetricPoint {
  /** epoch em milissegundos */
  t: number;
  cpuMillicores: number;
  memoryBytes: number;
}

export interface MetricsResponse {
  intervalSeconds: number;
  points: MetricPoint[];
}

const MEMORY_UNITS: Record<string, number> = {
  "": 1,
  Ki: 2 ** 10,
  Mi: 2 ** 20,
  Gi: 2 ** 30,
  Ti: 2 ** 40,
  K: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
};

export function parseCpuMillicores(quantity?: string): number | undefined {
  if (!quantity) return undefined;
  const m = /^(\d+(?:\.\d+)?)(m?)$/.exec(quantity.trim());
  if (!m) return undefined;
  const n = parseFloat(m[1]);
  return m[2] === "m" ? n : n * 1000;
}

export function parseMemoryBytes(quantity?: string): number | undefined {
  if (!quantity) return undefined;
  const m = /^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti|K|M|G|T)?$/.exec(quantity.trim());
  if (!m) return undefined;
  return parseFloat(m[1]) * MEMORY_UNITS[m[2] ?? ""];
}

export function formatCpu(millicores: number, withUnit = true): string {
  const cores = +(millicores / 1000).toFixed(2);
  return withUnit ? `${cores} cores` : String(cores);
}

export function formatBytes(bytes: number): string {
  if (bytes >= 2 ** 30) return `${+(bytes / 2 ** 30).toFixed(2)} GiB`;
  return `${Math.round(bytes / 2 ** 20)} MiB`;
}

export function memoryUnit(maxBytes: number): number {
  return maxBytes >= 2 ** 30 ? 2 ** 30 : 2 ** 20;
}

export function niceCeil(v: number): number {
  if (v <= 0) return 1;
  const exp = Math.floor(Math.log10(v));
  const f = v / 10 ** exp;
  const nf = f <= 1 ? 1 : f <= 2 ? 2 : f <= 4 ? 4 : f <= 8 ? 8 : 10;
  return nf * 10 ** exp;
}

function pseudo(i: number): number {
  const x = Math.sin(i * 12.9898) * 43758.5453;
  return x - Math.floor(x);
}

export function mockMetrics(range: MetricsRange, limits: { cpu?: number; memory?: number }): MetricsResponse {
  const n = 60;
  const span = RANGE_MS[range];
  const step = span / n;
  const now = Date.now();
  const cpuLimit = limits.cpu ?? 1000;
  const memLimit = limits.memory ?? 2 ** 30;

  const points: MetricPoint[] = Array.from({ length: n }, (_, i) => {
    const wave = Math.sin(i / 5);
    const cpu = cpuLimit * 0.35 * (1 + 0.5 * wave + 0.3 * (pseudo(i) - 0.5));
    const mem = memLimit * 0.5 * (1 + 0.25 * (i / n) + 0.03 * (pseudo(i + 100) - 0.5));
    return {
      t: now - span + (i + 1) * step,
      cpuMillicores: Math.max(0, Math.min(cpu, cpuLimit * 0.98)),
      memoryBytes: Math.max(0, Math.min(mem, memLimit * 0.98)),
    };
  });

  return { intervalSeconds: step / 1000, points };
}
