import {
  columnResizingFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  createColumnHelper,
  rowSortingFeature,
  tableFeatures,
  type ColumnDef,
  type ColumnSizingState,
  type ColumnVisibilityState,
} from "@tanstack/react-table";
import type { Torrent } from "@/api/types";
import {
  formatActiveRate,
  formatBytes,
  formatDuration,
  formatEta,
  formatRatio,
  formatUnixTime,
} from "@/lib/format";
import { torrentTrackerHost } from "@/lib/filters";
import { ProgressBar, StateBadge } from "./torrent-bits";

export interface ColumnMeta {
  /** The name in the column chooser. */
  label: string;
  align?: "right";
}

// Sorting is done in lib/sort.ts on the whole list; the table only tracks
// which column is sorted, sizes and visibility.
export const features = tableFeatures({
  columnVisibilityFeature,
  columnSizingFeature,
  columnResizingFeature,
  rowSortingFeature,
  columnMeta: {} as ColumnMeta,
});

const col = createColumnHelper<typeof features, Torrent>();

const right = { align: "right" } as const;

// The columns hold different value types; `any` is how TanStack Table
// expects a mixed list to be typed.
// oxlint-disable-next-line typescript/no-explicit-any
export const columns: ColumnDef<typeof features, Torrent, any>[] = [
  col.display({
    id: "select",
    size: 36,
    enableHiding: false,
    enableSorting: false,
    enableResizing: false,
    meta: { label: "Select" },
  }),
  col.accessor("name", {
    header: "Name",
    size: 320,
    minSize: 120,
    enableHiding: false,
    meta: { label: "Name" },
    cell: (c) => (
      <span className="truncate" title={c.row.original.name}>
        {c.row.original.name}
      </span>
    ),
  }),
  col.accessor("size", {
    header: "Size",
    size: 90,
    meta: { label: "Size", ...right },
    cell: (c) => formatBytes(c.row.original.size),
  }),
  col.accessor("totalSize", {
    header: "Total size",
    size: 90,
    meta: { label: "Total size", ...right },
    cell: (c) => formatBytes(c.row.original.totalSize),
  }),
  col.accessor("progress", {
    header: "Progress",
    size: 140,
    meta: { label: "Progress" },
    cell: (c) => <ProgressBar torrent={c.row.original} />,
  }),
  col.accessor("state", {
    header: "Status",
    size: 130,
    meta: { label: "Status" },
    cell: (c) => <StateBadge torrent={c.row.original} />,
  }),
  col.accessor("downloadRate", {
    header: "Down",
    size: 92,
    sortDescFirst: true,
    meta: { label: "Download rate", ...right },
    cell: (c) => formatActiveRate(c.row.original.downloadRate),
  }),
  col.accessor("uploadRate", {
    header: "Up",
    size: 92,
    sortDescFirst: true,
    meta: { label: "Upload rate", ...right },
    cell: (c) => formatActiveRate(c.row.original.uploadRate),
  }),
  col.accessor("eta", {
    header: "ETA",
    size: 76,
    meta: { label: "ETA", ...right },
    cell: (c) =>
      c.row.original.complete || c.row.original.stopped ? "" : formatEta(c.row.original.eta),
  }),
  col.accessor("ratio", {
    header: "Ratio",
    size: 64,
    sortDescFirst: true,
    meta: { label: "Ratio", ...right },
    cell: (c) => formatRatio(c.row.original.ratio),
  }),
  col.accessor("seeds", {
    header: "Seeds",
    size: 64,
    sortDescFirst: true,
    meta: { label: "Seeds", ...right },
    cell: (c) => c.row.original.seeds || "",
  }),
  col.accessor("peers", {
    header: "Peers",
    size: 64,
    sortDescFirst: true,
    meta: { label: "Peers", ...right },
    cell: (c) => c.row.original.peers || "",
  }),
  col.accessor("category", {
    header: "Category",
    size: 110,
    meta: { label: "Category" },
    cell: (c) => <span className="truncate">{c.row.original.category}</span>,
  }),
  col.accessor((t) => (t.tags ?? []).join(", "), {
    id: "tags",
    header: "Tags",
    size: 120,
    meta: { label: "Tags" },
    cell: (c) => <span className="truncate">{(c.row.original.tags ?? []).join(", ")}</span>,
  }),
  col.accessor("addedAt", {
    header: "Added",
    size: 150,
    sortDescFirst: true,
    meta: { label: "Added" },
    cell: (c) => formatUnixTime(c.row.original.addedAt),
  }),
  col.accessor("completedAt", {
    header: "Completed",
    size: 150,
    sortDescFirst: true,
    meta: { label: "Completed" },
    cell: (c) => (c.row.original.completedAt > 0 ? formatUnixTime(c.row.original.completedAt) : ""),
  }),
  col.accessor((t) => t.size - t.completed, {
    id: "remaining",
    header: "Remaining",
    size: 90,
    meta: { label: "Remaining", ...right },
    cell: (c) => {
      const left = c.row.original.size - c.row.original.completed;
      return left > 0 ? formatBytes(left) : "";
    },
  }),
  col.accessor("downloaded", {
    header: "Downloaded",
    size: 96,
    sortDescFirst: true,
    meta: { label: "Downloaded", ...right },
    cell: (c) => formatBytes(c.row.original.downloaded),
  }),
  col.accessor("uploaded", {
    header: "Uploaded",
    size: 96,
    sortDescFirst: true,
    meta: { label: "Uploaded", ...right },
    cell: (c) => formatBytes(c.row.original.uploaded),
  }),
  col.accessor("seedingTime", {
    header: "Seeding time",
    size: 100,
    sortDescFirst: true,
    meta: { label: "Seeding time", ...right },
    cell: (c) => (c.row.original.seedingTime > 0 ? formatDuration(c.row.original.seedingTime) : ""),
  }),
  col.accessor("lastActivityAt", {
    header: "Last activity",
    size: 150,
    sortDescFirst: true,
    meta: { label: "Last activity" },
    cell: (c) => formatUnixTime(c.row.original.lastActivityAt),
  }),
  col.accessor(torrentTrackerHost, {
    id: "tracker",
    header: "Tracker",
    size: 150,
    meta: { label: "Tracker" },
    cell: (c) => <span className="truncate">{torrentTrackerHost(c.row.original)}</span>,
  }),
  col.accessor("savePath", {
    header: "Save path",
    size: 200,
    meta: { label: "Save path" },
    cell: (c) => (
      <span className="truncate" title={c.row.original.savePath}>
        {c.row.original.savePath}
      </span>
    ),
  }),
];

export const defaultVisibility: ColumnVisibilityState = {
  totalSize: false,
  seeds: false,
  peers: false,
  tags: false,
  completedAt: false,
  remaining: false,
  downloaded: false,
  uploaded: false,
  seedingTime: false,
  lastActivityAt: false,
  tracker: false,
  savePath: false,
};

export const defaultSizing: ColumnSizingState = {};
