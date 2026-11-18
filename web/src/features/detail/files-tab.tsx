import { useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ChevronRightIcon, FileIcon, FolderIcon, FolderOpenIcon } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { cn } from "cn";
import { api } from "@/api/client";
import type { FilePriority, Torrent, TorrentFile } from "@/api/types";
import { OptionSelect } from "@/components/form/option-select";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { unwrap } from "@/lib/api";
import {
  buildFileTree,
  flattenTree,
  folderPaths,
  nodeProgress,
  priorities,
  priorityLabels,
  type FlatRow,
} from "@/lib/file-tree";
import { formatBytes, formatPercent } from "@/lib/format";
import { useSetFilePriority } from "@/lib/mutations";
import { keys } from "@/lib/queries";

const rowHeight = 34;
const mixed = "mixed";

const priorityOptions = priorities.map((p) => ({ value: String(p), label: priorityLabels[p] }));

export function FilesTab({ torrent }: { torrent: Torrent }) {
  const hash = torrent.hash;
  const files = useQuery({
    queryKey: keys.files(hash),
    queryFn: async (): Promise<TorrentFile[]> =>
      (await unwrap(api.GET("/torrents/{hash}/files", { params: { path: { hash } } }))).items ?? [],
    refetchInterval: torrent.complete ? 15_000 : 3_000,
  });
  const tree = useMemo(() => buildFileTree(files.data ?? []), [files.data]);
  // Top-level folders start open, so a single-folder torrent shows its files.
  const [expanded, setExpanded] = useState<ReadonlySet<string> | null>(null);
  const open = useMemo(
    () => expanded ?? new Set(tree.children.filter((c) => c.kind === "folder").map((c) => c.path)),
    [expanded, tree],
  );
  const rows = useMemo(() => flattenTree(tree, open), [tree, open]);
  const setPriority = useSetFilePriority(hash);
  const scrollRef = useRef<HTMLDivElement>(null);
  // The app doesn't use the React Compiler, which this rule is about.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 10,
  });

  if (files.isPending) {
    return (
      <div className="flex justify-center p-6">
        <Spinner />
      </div>
    );
  }
  if (!torrent.hasMetadata) {
    return (
      <p className="p-4 text-muted-foreground">
        The file list arrives with the torrent's metadata.
      </p>
    );
  }
  if (files.isError) {
    return <p className="p-4 text-destructive">Couldn't load the files: {files.error.message}</p>;
  }

  const toggle = (path: string) => {
    const next = new Set(open);
    if (next.has(path)) {
      next.delete(path);
    } else {
      next.add(path);
    }
    setExpanded(next);
  };
  const hasFolders = tree.children.some((c) => c.kind === "folder");

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b px-3 py-1.5 text-xs text-muted-foreground">
        <span className="tabular">
          {(files.data?.length ?? 0).toLocaleString()} files, {formatBytes(tree.size)}
        </span>
        <div className="flex-1" />
        {hasFolders && (
          <>
            <Button
              size="xs"
              variant="ghost"
              onClick={() => setExpanded(new Set(folderPaths(tree)))}
            >
              Expand all
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setExpanded(new Set())}>
              Collapse all
            </Button>
          </>
        )}
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto" role="tree" aria-label="Files">
        <div className="relative" style={{ height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map((item) => {
            const row = rows[item.index];
            if (!row) {
              return null;
            }
            return (
              <FileRow
                key={row.node.path}
                row={row}
                start={item.start}
                expanded={open.has(row.node.path)}
                onToggle={toggle}
                onPriority={(indices, priority) => setPriority.mutate({ indices, priority })}
              />
            );
          })}
        </div>
      </div>
    </div>
  );
}

function FileRow({
  row: { node, depth },
  start,
  expanded,
  onToggle,
  onPriority,
}: {
  row: FlatRow;
  start: number;
  expanded: boolean;
  onToggle: (path: string) => void;
  onPriority: (indices: number[], priority: FilePriority) => void;
}) {
  const progress = nodeProgress(node);
  const folder = node.kind === "folder";
  const skipped = node.priority === 0;
  return (
    <div
      role="treeitem"
      aria-expanded={folder ? expanded : undefined}
      aria-level={depth + 1}
      aria-selected={false}
      className={cn(
        "absolute top-0 left-0 flex w-full items-center gap-2 border-b border-border/50 pr-3 text-sm hover:bg-muted/50",
        skipped && "text-muted-foreground",
      )}
      style={{
        height: rowHeight,
        transform: `translateY(${start}px)`,
        paddingLeft: 8 + depth * 16,
      }}
    >
      {folder ? (
        <button
          type="button"
          onClick={() => onToggle(node.path)}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none"
          aria-label={`${expanded ? "Collapse" : "Expand"} ${node.name}`}
        >
          <ChevronRightIcon
            className={cn("size-3.5 shrink-0 transition-transform", expanded && "rotate-90")}
          />
          {expanded ? (
            <FolderOpenIcon className="size-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <FolderIcon className="size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate" title={node.path}>
            {node.name}
          </span>
        </button>
      ) : (
        <span className="flex min-w-0 flex-1 items-center gap-1.5 pl-5">
          <FileIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span
            className={cn("truncate", skipped && "line-through decoration-muted-foreground/50")}
            title={node.path}
          >
            {node.name}
          </span>
        </span>
      )}
      <span className="hidden w-20 shrink-0 text-right text-xs tabular sm:block">
        {formatBytes(node.size)}
      </span>
      <span className="hidden w-32 shrink-0 items-center gap-2 md:flex">
        <span className="relative h-1.5 flex-1 bg-muted" aria-hidden="true">
          <span
            className={cn(
              "absolute inset-y-0 left-0",
              progress >= 1 ? "bg-state-success" : "bg-state-progress",
            )}
            style={{ width: `${progress * 100}%` }}
          />
        </span>
        <span className="w-11 text-right text-xs text-muted-foreground tabular">
          {formatPercent(progress)}
        </span>
      </span>
      <OptionSelect
        aria-label={`Priority of ${node.name}`}
        value={node.priority === "mixed" ? mixed : String(node.priority)}
        options={
          node.priority === "mixed"
            ? [{ value: mixed, label: "Mixed" }, ...priorityOptions]
            : priorityOptions
        }
        onChange={(v) => v !== mixed && onPriority(node.indices, Number(v) as FilePriority)}
        className="h-7 w-24 shrink-0 text-xs data-[size=default]:h-7"
      />
    </div>
  );
}
