import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/api/client";
import type { Torrent } from "@/api/types";
import { Spinner } from "@/components/ui/spinner";
import { unwrap } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { keys } from "@/lib/queries";
import { useTheme } from "@/lib/theme";

/** Piece states as the API encodes them: 0 missing, 1 downloading, 2 complete, 3 checking. */
export interface PieceCounts {
  missing: number;
  downloading: number;
  complete: number;
  checking: number;
}

export function countPieces(states: string): PieceCounts {
  const c: PieceCounts = { missing: 0, downloading: 0, complete: 0, checking: 0 };
  for (let i = 0; i < states.length; i++) {
    switch (states.charCodeAt(i)) {
      case 49:
        c.downloading++;
        break;
      case 50:
        c.complete++;
        break;
      case 51:
        c.checking++;
        break;
      default:
        c.missing++;
    }
  }
  return c;
}

/** The side of one piece's square, so the map fits a width and a maximum height. */
export function cellSize(pieces: number, width: number, maxHeight: number): number {
  if (pieces <= 0 || width <= 0) {
    return 0;
  }
  for (let size = 12; size > 1; size--) {
    const perRow = Math.floor(width / size);
    if (Math.ceil(pieces / perRow) * size <= maxHeight) {
      return size;
    }
  }
  return 1;
}

const legend = [
  { key: "complete", label: "Have", color: "var(--state-success)" },
  { key: "downloading", label: "Downloading", color: "var(--state-progress)" },
  { key: "checking", label: "Checking", color: "var(--state-info)" },
  { key: "missing", label: "Missing", color: "var(--muted)" },
] as const;

export function PiecesTab({ torrent }: { torrent: Torrent }) {
  const hash = torrent.hash;
  const pieces = useQuery({
    queryKey: keys.pieces(hash),
    queryFn: async () =>
      (await unwrap(api.GET("/torrents/{hash}/pieces", { params: { path: { hash } } }))).states,
    refetchInterval: torrent.complete ? 15_000 : 2_000,
    enabled: torrent.hasMetadata,
  });
  const counts = useMemo(() => countPieces(pieces.data ?? ""), [pieces.data]);

  if (!torrent.hasMetadata) {
    return <p className="p-4 text-muted-foreground">Pieces are known once the metadata arrives.</p>;
  }
  if (pieces.isPending) {
    return (
      <div className="flex justify-center p-6">
        <Spinner />
      </div>
    );
  }
  if (pieces.isError) {
    return <p className="p-4 text-destructive">Couldn't load the pieces: {pieces.error.message}</p>;
  }
  return (
    <div className="flex flex-col gap-4 p-4">
      <dl className="flex flex-wrap gap-x-6 gap-y-2 text-sm">
        {legend.map((l) => (
          <div key={l.key} className="flex items-center gap-2">
            <span
              className="size-3 border border-border"
              style={{ background: l.color }}
              aria-hidden="true"
            />
            <dt className="text-muted-foreground">{l.label}</dt>
            <dd className="tabular">{counts[l.key].toLocaleString()}</dd>
          </div>
        ))}
        <div className="flex items-center gap-2">
          <dt className="text-muted-foreground">Piece size</dt>
          <dd className="tabular">{formatBytes(torrent.pieceSize)}</dd>
        </div>
      </dl>
      <PieceMap states={pieces.data} />
    </div>
  );
}

function PieceMap({ states }: { states: string }) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [width, setWidth] = useState(0);
  const { resolved } = useTheme();

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) {
      return;
    }
    const ro = new ResizeObserver(([entry]) => setWidth(Math.floor(entry?.contentRect.width ?? 0)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const size = cellSize(states.length, width, 360);
  const perRow = size > 0 ? Math.floor(width / size) : 0;
  const height = perRow > 0 ? Math.ceil(states.length / perRow) * size : 0;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || size === 0) {
      return;
    }
    const dpr = globalThis.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      return;
    }
    ctx.scale(dpr, dpr);
    // The colours come from the theme, so draw again when it changes.
    canvas.dataset.theme = resolved;
    const css = getComputedStyle(document.documentElement);
    const colors = [
      css.getPropertyValue("--muted").trim(),
      css.getPropertyValue("--state-progress").trim(),
      css.getPropertyValue("--state-success").trim(),
      css.getPropertyValue("--state-info").trim(),
    ];
    const gap = size >= 4 ? 1 : 0;
    ctx.clearRect(0, 0, width, height);
    for (let i = 0; i < states.length; i++) {
      ctx.fillStyle = colors[states.charCodeAt(i) - 48] ?? colors[0] ?? "#888";
      ctx.fillRect((i % perRow) * size, Math.floor(i / perRow) * size, size - gap, size - gap);
    }
  }, [states, width, height, size, perRow, resolved]);

  return (
    <div ref={wrapRef} className="w-full">
      <canvas
        ref={canvasRef}
        role="img"
        aria-label={`Map of ${states.length.toLocaleString()} pieces`}
        style={{ width, height }}
      />
    </div>
  );
}
