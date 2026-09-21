export type ByteUnit = "Mi" | "Gi";
export interface ByteQuantity {
  value: number;
  unit: ByteUnit;
}

const BYTE_FACTORS: Record<string, number> = {
  "": 1,
  Ki: 1024,
  Mi: 1024 ** 2,
  Gi: 1024 ** 3,
  Ti: 1024 ** 4,
  k: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
};

export function parseBytes(q: string | undefined): number {
  const m = /^(\d+(?:\.\d+)?)([A-Za-z]*)$/.exec((q ?? "").trim());
  if (!m) return 0;
  return Number(m[1]) * (BYTE_FACTORS[m[2]] ?? 0);
}

export function parseCpu(q: string | undefined): number {
  const m = /^(\d+(?:\.\d+)?)(m?)$/.exec((q ?? "").trim());
  if (!m) return 0;
  return m[2] === "m" ? Number(m[1]) / 1000 : Number(m[1]);
}

export function splitBytes(q: string | undefined, fallback: ByteQuantity): ByteQuantity {
  const m = /^(\d+(?:\.\d+)?)(Mi|Gi)$/.exec((q ?? "").trim());
  return m ? { value: Number(m[1]), unit: m[2] as ByteUnit } : fallback;
}

export function joinBytes(q: ByteQuantity): string {
  return `${q.value}${q.unit}`;
}

export function toBytes(q: ByteQuantity): number {
  return q.value * BYTE_FACTORS[q.unit];
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1024 ** 3) return `${+(bytes / 1024 ** 3).toFixed(1)}Gi`;
  return `${Math.round(bytes / 1024 ** 2)}Mi`;
}
