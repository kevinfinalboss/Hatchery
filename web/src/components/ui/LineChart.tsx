import { type KeyboardEvent, type PointerEvent, type ReactNode, useEffect, useRef, useState } from "react";
import clsx from "clsx";

export interface ChartPoint {
  t: number;
  v: number;
}

interface LineChartProps {
  points: ChartPoint[];
  color: string;
  yMax: number;
  formatValue: (v: number) => string;
  formatTick: (v: number) => string;
  formatTime: (t: number) => string;
  limit?: number;
  limitLabel?: string;
  label: string;
  dim?: boolean;
  overlay?: ReactNode;
  height?: number;
}

const PAD = { top: 10, right: 14, bottom: 24, left: 52 };
const Y_TICKS = 4;
const X_TICKS = 3;
const TOOLTIP_W = 150;

export function LineChart({
  points,
  color,
  yMax,
  formatValue,
  formatTick,
  formatTime,
  limit,
  limitLabel,
  label,
  dim,
  overlay,
  height = 200,
}: LineChartProps) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [active, setActive] = useState<number | null>(null);

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setWidth(el.clientWidth));
    observer.observe(el);
    setWidth(el.clientWidth);
    return () => observer.disconnect();
  }, []);

  const innerW = Math.max(0, width - PAD.left - PAD.right);
  const innerH = height - PAD.top - PAD.bottom;
  const tMin = points[0]?.t ?? 0;
  const tMax = points[points.length - 1]?.t ?? 0;
  const xOf = (t: number) => PAD.left + (tMax === tMin ? innerW : ((t - tMin) / (tMax - tMin)) * innerW);
  const yOf = (v: number) => PAD.top + innerH - (Math.min(Math.max(v, 0), yMax) / yMax) * innerH;
  const xs = points.map((p) => xOf(p.t));

  const linePath = points.map((p, i) => `${i === 0 ? "M" : "L"}${xs[i].toFixed(1)} ${yOf(p.v).toFixed(1)}`).join(" ");
  const baseY = PAD.top + innerH;
  const areaPath = points.length > 1 ? `${linePath} L${xs[xs.length - 1].toFixed(1)} ${baseY} L${xs[0].toFixed(1)} ${baseY} Z` : "";

  function nearest(px: number): number {
    let best = 0;
    let bestDist = Infinity;
    for (let i = 0; i < xs.length; i++) {
      const d = Math.abs(xs[i] - px);
      if (d < bestDist) {
        bestDist = d;
        best = i;
      }
    }
    return best;
  }

  function onPointerMove(e: PointerEvent<SVGSVGElement>) {
    if (points.length === 0) return;
    setActive(nearest(e.clientX - e.currentTarget.getBoundingClientRect().left));
  }

  function onKeyDown(e: KeyboardEvent<SVGSVGElement>) {
    if (points.length === 0) return;
    if (e.key === "ArrowLeft") {
      e.preventDefault();
      setActive((i) => Math.max(0, (i ?? points.length) - 1));
    } else if (e.key === "ArrowRight") {
      e.preventDefault();
      setActive((i) => Math.min(points.length - 1, (i ?? -1) + 1));
    } else if (e.key === "Escape") {
      setActive(null);
    }
  }

  const activePoint = active !== null ? points[active] : undefined;
  const activeX = active !== null ? xs[active] : 0;
  const tooltipLeft = activeX + 12 + TOOLTIP_W > width ? activeX - 12 - TOOLTIP_W : activeX + 12;

  const yTicks = Array.from({ length: Y_TICKS + 1 }, (_, i) => (yMax / Y_TICKS) * i);
  const xTicks =
    points.length > 1 ? Array.from({ length: X_TICKS + 1 }, (_, i) => tMin + ((tMax - tMin) / X_TICKS) * i) : [];
  const showLimit = limit !== undefined && limit > 0 && limit <= yMax;

  return (
    <div
      ref={wrapRef}
      className={clsx("relative w-full transition-opacity", dim && "opacity-60")}
      style={{ height }}
    >
      {width > 0 && (
        <svg
          width={width}
          height={height}
          role="img"
          aria-label={label}
          tabIndex={0}
          onPointerMove={onPointerMove}
          onPointerLeave={() => setActive(null)}
          onKeyDown={onKeyDown}
          onFocus={() => points.length > 0 && setActive((i) => i ?? points.length - 1)}
          onBlur={() => setActive(null)}
          className="block font-sans outline-none focus-visible:ring-2 focus-visible:ring-primary"
          style={{ touchAction: "pan-y" }}
        >
          {yTicks.map((v) => (
            <g key={v}>
              <line x1={PAD.left} x2={width - PAD.right} y1={yOf(v)} y2={yOf(v)} stroke="var(--c-border)" strokeWidth={1} />
              <text x={PAD.left - 8} y={yOf(v)} textAnchor="end" dominantBaseline="middle" fontSize={10} fill="var(--c-text-tertiary)">
                {formatTick(v)}
              </text>
            </g>
          ))}
          {xTicks.map((t, i) => (
            <text
              key={t}
              x={xOf(t)}
              y={height - 6}
              textAnchor={i === 0 ? "start" : i === xTicks.length - 1 ? "end" : "middle"}
              fontSize={10}
              fill="var(--c-text-tertiary)"
            >
              {formatTime(t)}
            </text>
          ))}

          {showLimit && (
            <g>
              <line x1={PAD.left} x2={width - PAD.right} y1={yOf(limit)} y2={yOf(limit)} stroke="var(--c-text-tertiary)" strokeWidth={1} />
              <text x={width - PAD.right} y={yOf(limit) - 5} textAnchor="end" fontSize={10} fill="var(--c-text-tertiary)">
                {limitLabel}
              </text>
            </g>
          )}

          {areaPath && <path d={areaPath} fill={color} opacity={0.1} />}
          {points.length > 0 && (
            <path d={linePath} fill="none" stroke={color} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          )}
          {points.length > 0 && active === null && (
            <circle cx={xs[xs.length - 1]} cy={yOf(points[points.length - 1].v)} r={4} fill={color} stroke="var(--c-surface)" strokeWidth={2} />
          )}

          {activePoint && (
            <g>
              <line x1={activeX} x2={activeX} y1={PAD.top} y2={baseY} stroke="var(--c-border-strong)" strokeWidth={1} />
              <circle cx={activeX} cy={yOf(activePoint.v)} r={4} fill={color} stroke="var(--c-surface)" strokeWidth={2} />
            </g>
          )}
        </svg>
      )}

      {overlay && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center px-6 text-center font-prose text-sm text-text-secondary">
          {overlay}
        </div>
      )}

      {activePoint && (
        <div
          className="pointer-events-none absolute top-2 border border-border-strong bg-surface px-2.5 py-1.5"
          style={{ left: Math.max(0, tooltipLeft), width: TOOLTIP_W }}
        >
          <div className="flex items-center gap-2">
            <span aria-hidden className="inline-block h-0.5 w-3" style={{ background: color }} />
            <span className="font-sans text-sm font-semibold text-text-primary">{formatValue(activePoint.v)}</span>
          </div>
          <div className="mt-0.5 font-sans text-xs text-text-secondary">{formatTime(activePoint.t)}</div>
        </div>
      )}
    </div>
  );
}
