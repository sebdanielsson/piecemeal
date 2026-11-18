import {
  flexRender,
  useTable,
  type Column,
  type ColumnSizingState,
  type Row,
  type SortingState,
  type Updater,
  type ColumnVisibilityState,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "cn";
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react";
import { memo, useEffect, useMemo, useRef, type CSSProperties, type MouseEvent } from "react";
import type { Torrent } from "@/api/types";
import { Checkbox } from "@/components/ui/checkbox";
import { formatActiveRate, formatBytes, formatEta } from "@/lib/format";
import { columns, features } from "./columns";
import { ProgressBar, StateBadge } from "./torrent-bits";

export interface RowHandlers {
  onClick: (hash: string, e: MouseEvent) => void;
  onToggle: (hash: string, range: boolean) => void;
  onOpen: (hash: string) => void;
  onContextMenu: (hash: string) => void;
}

interface ListProps {
  rows: Torrent[];
  selected: ReadonlySet<string>;
  activeHash: string | undefined;
  cursor: string | null;
  handlers: RowHandlers;
  allSelected: boolean | "mixed";
  onToggleAll: () => void;
}

const rowHeight = 34;
const cardHeight = 84;

/** Scrolls the cursor's row into view when the keyboard moves it. */
function useScrollToCursor(
  virtualizer: { scrollToIndex: (i: number, o: { align: "auto" }) => void },
  rows: Torrent[],
  cursor: string | null,
) {
  const last = useRef<string | null>(null);
  useEffect(() => {
    if (cursor === null || cursor === last.current) {
      return;
    }
    last.current = cursor;
    const i = rows.findIndex((t) => t.hash === cursor);
    if (i >= 0) {
      virtualizer.scrollToIndex(i, { align: "auto" });
    }
  }, [cursor, rows, virtualizer]);
}

function rowId(hash: string) {
  return `torrent-${hash}`;
}

// ---- Desktop: a virtualized table --------------------------------------

export function TorrentTable({
  rows,
  selected,
  activeHash,
  cursor,
  handlers,
  allSelected,
  onToggleAll,
  sorting,
  onSortingChange,
  columnVisibility,
  onColumnVisibilityChange,
  columnSizing,
  onColumnSizingChange,
}: ListProps & {
  sorting: SortingState;
  onSortingChange: (u: Updater<SortingState>) => void;
  columnVisibility: ColumnVisibilityState;
  onColumnVisibilityChange: (u: Updater<ColumnVisibilityState>) => void;
  columnSizing: ColumnSizingState;
  onColumnSizingChange: (u: Updater<ColumnSizingState>) => void;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const table = useTable(
    {
      features,
      columns,
      data: rows,
      getRowId: (t) => t.hash,
      state: { sorting, columnVisibility, columnSizing },
      onSortingChange,
      onColumnVisibilityChange,
      onColumnSizingChange,
      manualSorting: true,
      enableSortingRemoval: false,
      enableMultiSort: false,
      columnResizeMode: "onChange",
    },
    (s) => ({ columnResizing: s.columnResizing }),
  );
  const tableRows = table.getRowModel().rows;
  // The app doesn't use the React Compiler, which this rule is about.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: tableRows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 12,
  });
  useScrollToCursor(virtualizer, rows, cursor);

  const headers = table.getHeaderGroups()[0]?.headers ?? [];
  const leafColumns = table.getVisibleLeafColumns();
  const totalWidth = leafColumns.reduce((sum, c) => sum + c.getSize(), 0);
  // Rows re-render when the columns or their widths change.
  const columnsKey = leafColumns.map((c) => `${c.id}:${c.getSize()}`).join(",");

  return (
    <div
      ref={scrollRef}
      role="grid"
      aria-label="Torrents"
      aria-rowcount={rows.length + 1}
      aria-multiselectable="true"
      aria-activedescendant={cursor ? rowId(cursor) : undefined}
      tabIndex={0}
      className="relative min-h-0 flex-1 overflow-auto text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:ring-inset"
    >
      <div
        role="row"
        aria-rowindex={1}
        className="sticky top-0 z-10 flex h-8 min-w-full border-b bg-background text-xs font-semibold tracking-wider text-muted-foreground uppercase"
        style={{ width: totalWidth }}
      >
        {headers.map((header) => {
          const column = header.column;
          const sorted = column.getIsSorted();
          const align = column.columnDef.meta?.align;
          if (column.id === "select") {
            return (
              <div
                key={header.id}
                role="columnheader"
                className="flex items-center justify-center"
                style={cellStyle(column)}
              >
                <Checkbox
                  aria-label="Select all"
                  checked={allSelected === true}
                  indeterminate={allSelected === "mixed"}
                  onCheckedChange={onToggleAll}
                  disabled={rows.length === 0}
                />
              </div>
            );
          }
          return (
            <div
              key={header.id}
              role="columnheader"
              aria-sort={
                sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : undefined
              }
              className="group/header relative flex items-center"
              style={cellStyle(column)}
            >
              <button
                type="button"
                onClick={column.getToggleSortingHandler()}
                className={cn(
                  "flex h-full min-w-0 flex-1 items-center gap-1 px-2 uppercase hover:text-foreground focus-visible:text-foreground focus-visible:outline-none",
                  align === "right" && "justify-end",
                  sorted && "text-foreground",
                )}
              >
                <span className="truncate">
                  {flexRender(column.columnDef.header, header.getContext())}
                </span>
                {sorted === "asc" && <ArrowUpIcon className="size-3 shrink-0" />}
                {sorted === "desc" && <ArrowDownIcon className="size-3 shrink-0" />}
              </button>
              {column.getCanResize() && (
                <div
                  role="separator"
                  aria-orientation="vertical"
                  aria-label={`Resize ${column.columnDef.meta?.label ?? column.id}`}
                  onMouseDown={header.getResizeHandler()}
                  onTouchStart={header.getResizeHandler()}
                  onDoubleClick={() => column.resetSize()}
                  className={cn(
                    "absolute top-1.5 right-0 bottom-1.5 w-1.5 cursor-col-resize touch-none select-none",
                    "after:absolute after:inset-y-0 after:right-0 after:w-px after:bg-border group-hover/header:after:bg-ring",
                    column.getIsResizing() && "after:bg-ring",
                  )}
                />
              )}
            </div>
          );
        })}
      </div>
      <div
        className="relative min-w-full"
        style={{ height: virtualizer.getTotalSize(), width: totalWidth }}
      >
        {virtualizer.getVirtualItems().map((item) => {
          const row = tableRows[item.index];
          if (!row) {
            return null;
          }
          const hash = row.original.hash;
          return (
            <TableRow
              key={hash}
              row={row}
              torrent={row.original}
              index={item.index}
              start={item.start}
              selected={selected.has(hash)}
              active={activeHash === hash}
              isCursor={cursor === hash}
              columnsKey={columnsKey}
              handlers={handlers}
            />
          );
        })}
      </div>
    </div>
  );
}

type TorrentColumn = Column<typeof features, Torrent, unknown>;

function cellStyle(column: TorrentColumn): CSSProperties {
  const size = column.getSize();
  // The name column takes whatever width is left.
  return column.id === "name"
    ? { flex: `1 0 ${size}px`, minWidth: size }
    : { width: size, flex: "none" };
}

interface TableRowProps {
  row: Row<typeof features, Torrent>;
  torrent: Torrent;
  index: number;
  start: number;
  selected: boolean;
  active: boolean;
  isCursor: boolean;
  columnsKey: string;
  handlers: RowHandlers;
}

// The row object is rebuilt on every list update, but what it renders only
// depends on the torrent, which keeps its identity until it changes.
const TableRow = memo(
  function TableRow({
    row,
    torrent,
    index,
    start,
    selected,
    active,
    isCursor,
    handlers,
  }: TableRowProps) {
    const hash = torrent.hash;
    return (
      <div
        id={rowId(hash)}
        role="row"
        aria-rowindex={index + 2}
        aria-selected={selected}
        data-selected={selected || undefined}
        data-active={active || undefined}
        onClick={(e) => handlers.onClick(hash, e)}
        onDoubleClick={() => handlers.onOpen(hash)}
        onContextMenu={() => handlers.onContextMenu(hash)}
        className={cn(
          "absolute top-0 left-0 flex w-full cursor-default items-center border-b border-border/50 select-none",
          "hover:bg-muted/60 data-selected:bg-accent data-active:bg-accent data-active:shadow-[inset_2px_0_0_var(--primary)]",
          isCursor && "outline-1 -outline-offset-1 outline-ring/60",
        )}
        style={{ height: rowHeight, transform: `translateY(${start}px)` }}
      >
        {row.getVisibleCells().map((cell) => {
          const column = cell.column;
          if (column.id === "select") {
            return (
              <div
                key={cell.id}
                role="gridcell"
                className="flex items-center justify-center"
                style={cellStyle(column)}
                onClick={(e) => {
                  e.stopPropagation();
                  handlers.onToggle(hash, e.shiftKey);
                }}
                onDoubleClick={(e) => e.stopPropagation()}
              >
                <Checkbox checked={selected} aria-label={`Select ${torrent.name}`} tabIndex={-1} />
              </div>
            );
          }
          return (
            <div
              key={cell.id}
              role="gridcell"
              className={cn(
                "flex min-w-0 items-center overflow-hidden px-2 whitespace-nowrap",
                column.columnDef.meta?.align === "right" && "justify-end tabular",
              )}
              style={cellStyle(column)}
            >
              {flexRender(column.columnDef.cell, cell.getContext())}
            </div>
          );
        })}
      </div>
    );
  },
  (a, b) =>
    a.torrent === b.torrent &&
    a.index === b.index &&
    a.start === b.start &&
    a.selected === b.selected &&
    a.active === b.active &&
    a.isCursor === b.isCursor &&
    a.columnsKey === b.columnsKey &&
    a.handlers === b.handlers,
);

// ---- Phone width: a virtualized list of cards ------------------------------

export function TorrentCards({ rows, selected, activeHash, cursor, handlers }: ListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  // The app doesn't use the React Compiler, which this rule is about.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => cardHeight,
    overscan: 6,
  });
  useScrollToCursor(virtualizer, rows, cursor);
  return (
    <div
      ref={scrollRef}
      role="list"
      aria-label="Torrents"
      className="relative min-h-0 flex-1 overflow-auto"
    >
      <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((item) => {
          const torrent = rows[item.index];
          if (!torrent) {
            return null;
          }
          return (
            <TorrentCard
              key={torrent.hash}
              torrent={torrent}
              start={item.start}
              selected={selected.has(torrent.hash)}
              active={activeHash === torrent.hash}
              handlers={handlers}
            />
          );
        })}
      </div>
    </div>
  );
}

const TorrentCard = memo(function TorrentCard({
  torrent: t,
  start,
  selected,
  active,
  handlers,
}: {
  torrent: Torrent;
  start: number;
  selected: boolean;
  active: boolean;
  handlers: RowHandlers;
}) {
  const rates = [
    t.downloadRate > 0 && `↓ ${formatActiveRate(t.downloadRate)}`,
    t.uploadRate > 0 && `↑ ${formatActiveRate(t.uploadRate)}`,
    !t.complete && !t.stopped && t.eta > 0 && formatEta(t.eta),
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <div
      role="listitem"
      aria-selected={selected}
      data-selected={selected || undefined}
      data-active={active || undefined}
      className="absolute top-0 left-0 flex w-full items-stretch gap-3 border-b px-3 py-2.5 data-selected:bg-accent data-active:bg-accent"
      style={{ height: cardHeight, transform: `translateY(${start}px)` }}
      onContextMenu={() => handlers.onContextMenu(t.hash)}
    >
      <div className="flex items-center">
        <Checkbox
          checked={selected}
          onCheckedChange={() => handlers.onToggle(t.hash, false)}
          aria-label={`Select ${t.name}`}
        />
      </div>
      <button
        type="button"
        className="flex min-w-0 flex-1 flex-col justify-between text-left focus-visible:outline-none"
        onClick={() => handlers.onOpen(t.hash)}
      >
        <span className="truncate text-sm font-medium">{t.name}</span>
        <ProgressBar torrent={t} />
        <span className="flex min-w-0 items-center justify-between gap-2 text-xs text-muted-foreground">
          <StateBadge torrent={t} className="shrink-0" />
          <span className="truncate tabular">
            {formatBytes(t.completed)} of {formatBytes(t.size)}
            {rates && ` · ${rates}`}
          </span>
        </span>
      </button>
    </div>
  );
});

export function useAllSelected(rows: Torrent[], selected: ReadonlySet<string>): boolean | "mixed" {
  return useMemo(() => {
    if (selected.size === 0 || rows.length === 0) {
      return false;
    }
    let n = 0;
    for (const t of rows) {
      if (selected.has(t.hash)) {
        n++;
      }
    }
    return n === 0 ? false : n === rows.length ? true : "mixed";
  }, [rows, selected]);
}
