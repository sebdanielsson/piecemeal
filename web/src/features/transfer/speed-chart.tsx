import { useEffect, useMemo, useRef, useState, type PointerEvent } from "react";
import { formatRate } from "@/lib/format";
import type { TransferSample } from "@/lib/live/reducer";

/** A round upper bound for the y axis: 1, 2 or 5 times a power of 1024 steps. */
export function niceCeiling(max: number): number {
  if (max <= 0) {
    return 100 * 1024;
  }
  const base = 1024 ** Math.floor(Math.log(max) / Math.log(1024));
  for (const step of [1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 1024]) {
    if (step * base >= max) {
      return step * base;
    }
  }
  return 1024 * base;
}

const tickSteps = [10_000, 15_000, 30_000, 60_000, 120_000, 300_000, 600_000];

/** Offsets in milliseconds before now for the x axis: round steps, at most six ticks. */
export function timeTicks(windowMs: number): number[] {
  const step = tickSteps.find((s) => windowMs / s <= 5) ?? windowMs / 5;
  const out: number[] = [];
  for (let ago = 0; ago <= windowMs + 1; ago += step) {
    out.push(ago);
  }
  return out;
}

/** An x axis label: "now", "−30s", "−5m" or "−1m 30s". */
export function formatAgo(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s === 0) {
    return "now";
  }
  if (s < 60) {
    return `−${s}s`;
  }
  return s % 60 === 0 ? `−${s / 60}m` : `−${Math.floor(s / 60)}m ${s % 60}s`;
}

const height = 220;
const pad = { top: 12, right: 12, bottom: 22, left: 76 };

/** Download and upload rates over the last minutes, from the event stream. */
export function SpeedChart({ samples, windowMs }: { samples: TransferSample[]; windowMs: number }) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(600);
  const [hover, setHover] = useState<number | null>(null);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) {
      return;
    }
    const ro = new ResizeObserver(([e]) =>
      setWidth(Math.max(240, Math.floor(e?.contentRect.width ?? 600))),
    );
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const now = samples.at(-1)?.at ?? 0;
  const start = now - windowMs;
  const visible = useMemo(() => samples.filter((s) => s.at >= start), [samples, start]);
  const max = niceCeiling(Math.max(0, ...visible.map((s) => Math.max(s.down, s.up))));
  const plotW = width - pad.left - pad.right;
  const plotH = height - pad.top - pad.bottom;
  const x = (at: number) => pad.left + ((at - start) / windowMs) * plotW;
  const y = (v: number) => pad.top + plotH - (v / max) * plotH;

  const path = (pick: (s: TransferSample) => number) =>
    visible
      .map((s, i) => `${i === 0 ? "M" : "L"}${x(s.at).toFixed(1)},${y(pick(s)).toFixed(1)}`)
      .join("");
  const area = (pick: (s: TransferSample) => number) => {
    const first = visible[0];
    const last = visible.at(-1);
    if (!first || !last) {
      return "";
    }
    return `${path(pick)}L${x(last.at).toFixed(1)},${y(0)}L${x(first.at).toFixed(1)},${y(0)}Z`;
  };

  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * max);
  const minutes = windowMs / 60_000;
  const windowText = minutes === 1 ? "minute" : `${minutes} minutes`;
  const xTicks = timeTicks(windowMs).map((ago) => ({
    f: 1 - ago / windowMs,
    label: formatAgo(ago),
  }));

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    if (visible.length === 0) {
      return;
    }
    const rect = e.currentTarget.getBoundingClientRect();
    const at = start + ((e.clientX - rect.left - pad.left) / plotW) * windowMs;
    let best = 0;
    for (let i = 1; i < visible.length; i++) {
      if (Math.abs((visible[i]?.at ?? 0) - at) < Math.abs((visible[best]?.at ?? 0) - at)) {
        best = i;
      }
    }
    setHover(best);
  };
  const point = hover !== null ? visible[hover] : undefined;
  const last = visible.at(-1);

  return (
    <div ref={wrapRef} className="w-full">
      <svg
        width={width}
        height={height}
        role="img"
        aria-label={
          last
            ? `Transfer rates over the last ${windowText}. Now: download ${formatRate(last.down)}, upload ${formatRate(last.up)}.`
            : "Transfer rates: no data yet."
        }
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
        className="touch-none select-none"
      >
        {ticks.map((t) => (
          <g key={t}>
            <line
              x1={pad.left}
              x2={width - pad.right}
              y1={y(t)}
              y2={y(t)}
              className="stroke-border"
              strokeWidth={1}
            />
            <text
              x={pad.left - 8}
              y={y(t)}
              dy="0.32em"
              textAnchor="end"
              className="fill-muted-foreground text-[10px] tabular"
            >
              {formatRate(t)}
            </text>
          </g>
        ))}
        {xTicks.map((t) => (
          <text
            key={t.f}
            x={pad.left + t.f * plotW}
            y={height - 6}
            textAnchor={t.f === 0 ? "start" : t.f === 1 ? "end" : "middle"}
            className="fill-muted-foreground text-[10px]"
          >
            {t.label}
          </text>
        ))}
        {visible.length > 1 && (
          <>
            <path d={area((s) => s.down)} className="fill-download/15" />
            <path d={area((s) => s.up)} className="fill-upload/15" />
            <path
              d={path((s) => s.down)}
              className="fill-none stroke-download"
              strokeWidth={1.75}
              strokeLinejoin="round"
            />
            <path
              d={path((s) => s.up)}
              className="fill-none stroke-upload"
              strokeWidth={1.75}
              strokeLinejoin="round"
            />
          </>
        )}
        {point && (
          <g>
            <line
              x1={x(point.at)}
              x2={x(point.at)}
              y1={pad.top}
              y2={pad.top + plotH}
              className="stroke-foreground/40"
              strokeDasharray="3 3"
            />
            <circle cx={x(point.at)} cy={y(point.down)} r={3} className="fill-download" />
            <circle cx={x(point.at)} cy={y(point.up)} r={3} className="fill-upload" />
          </g>
        )}
      </svg>
      <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-sm" aria-live="off">
        <span className="flex items-center gap-2">
          <span className="h-0.5 w-4 bg-download" aria-hidden="true" />
          Download{" "}
          <span className="tabular font-medium">{formatRate((point ?? last)?.down ?? 0)}</span>
        </span>
        <span className="flex items-center gap-2">
          <span className="h-0.5 w-4 bg-upload" aria-hidden="true" />
          Upload <span className="tabular font-medium">{formatRate((point ?? last)?.up ?? 0)}</span>
        </span>
        {point && (
          <span className="text-muted-foreground tabular">
            {new Date(point.at).toLocaleTimeString()}
          </span>
        )}
      </div>
    </div>
  );
}
