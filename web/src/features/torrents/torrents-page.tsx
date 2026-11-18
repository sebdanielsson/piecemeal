import type { SortingState, Updater, ColumnVisibilityState } from "@tanstack/react-table";
import { Link, Outlet, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import {
  Columns3Icon,
  EllipsisIcon,
  FilterXIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  SearchIcon,
  XIcon,
} from "lucide-react";
import {
  useCallback,
  useDeferredValue,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
} from "react";
import type { Torrent } from "@/api/types";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Kbd } from "@/components/ui/kbd";
import { Spinner } from "@/components/ui/spinner";
import { useIsMobile } from "@/hooks/use-mobile";
import { usePersistentState } from "@/hooks/use-persistent-state";
import { columns, defaultVisibility } from "./columns";
import { filterTorrents, stateFilterLabels } from "@/lib/filters";
import { plural } from "@/lib/format";
import { useHotkeys } from "@/lib/hotkeys";
import { useTorrentMap } from "@/lib/live/hooks";
import { useTorrentAction } from "@/lib/mutations";
import { sortTorrents } from "@/lib/sort";
import { openAddDialog } from "./add-store";
import { NewTagDialog } from "./new-tag-dialog";
import { RemoveDialog } from "./remove-dialog";
import { listFilterFrom, type ListSearch } from "./search";
import {
  clickSelect,
  emptySelection,
  moveCursor,
  selectAll,
  visibleSelection,
  type Selection,
} from "./selection";
import { DetailSplit } from "./detail-split";
import { shouldStart, TorrentActionItems } from "./torrent-actions";
import { TorrentCards, TorrentTable, useAllSelected, type RowHandlers } from "./torrent-list";

const isSorting = (v: unknown): v is SortingState =>
  Array.isArray(v) &&
  v.every(
    (s) =>
      typeof s === "object" &&
      s !== null &&
      typeof s.id === "string" &&
      typeof s.desc === "boolean",
  );

const isRecord = (v: unknown): v is Record<string, never> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

const applyUpdater =
  <T,>(set: (fn: (p: T) => T) => void) =>
  (u: Updater<T>) =>
    set((prev) => (typeof u === "function" ? (u as (p: T) => T)(prev) : u));

export function TorrentsPage() {
  const search = useSearch({ from: "/app/list" });
  const navigate = useNavigate();
  const { hash: activeHash } = useParams({ strict: false }) as { hash?: string };
  const torrents = useTorrentMap();
  const isMobile = useIsMobile();

  // The search box filters as you type; the URL follows a moment later.
  // Back and forward change the URL first, and the box follows.
  const urlQuery = search.q ?? "";
  const [query, setQuery] = useState(urlQuery);
  const [seenUrlQuery, setSeenUrlQuery] = useState(urlQuery);
  if (urlQuery !== seenUrlQuery) {
    setSeenUrlQuery(urlQuery);
    setQuery(urlQuery);
  }
  const deferredQuery = useDeferredValue(query);
  useEffect(() => {
    if (query === urlQuery) {
      return;
    }
    const id = setTimeout(() => {
      void navigate({
        to: ".",
        search: (prev: ListSearch) => ({ ...prev, q: query || undefined }),
        replace: true,
      });
    }, 300);
    return () => clearTimeout(id);
  }, [query, urlQuery, navigate]);

  const [sorting, setSorting] = usePersistentState<SortingState>(
    "piecemeal.list.sorting",
    [{ id: "addedAt", desc: true }],
    isSorting,
  );
  const [columnVisibility, setColumnVisibility] = usePersistentState<ColumnVisibilityState>(
    "piecemeal.list.columns",
    defaultVisibility,
    isRecord,
  );
  const [columnSizing, setColumnSizing] = usePersistentState<Record<string, number>>(
    "piecemeal.list.sizes",
    {},
    isRecord,
  );
  const filter = useMemo(
    () => ({ ...listFilterFrom(search), search: deferredQuery }),
    [search, deferredQuery],
  );
  const rows = useMemo(
    () => (torrents ? sortTorrents(filterTorrents(torrents.values(), filter), sorting[0]) : []),
    [torrents, filter, sorting],
  );
  const order = useMemo(() => rows.map((t) => t.hash), [rows]);

  const [selection, setSelection] = useState<Selection>(() =>
    activeHash
      ? { selected: new Set([activeHash]), anchor: activeHash, cursor: activeHash }
      : emptySelection,
  );
  const selectedHashes = useMemo(() => visibleSelection(selection, order), [selection, order]);
  const selectedTorrents = useMemo(
    () => selectedHashes.map((h) => torrents?.get(h)).filter((t): t is Torrent => t !== undefined),
    [selectedHashes, torrents],
  );
  const allSelected = useAllSelected(rows, selection.selected);

  // The cursor follows the open torrent, such as one opened from a link.
  const [seenActive, setSeenActive] = useState(activeHash);
  if (activeHash !== seenActive) {
    setSeenActive(activeHash);
    if (activeHash && selection.cursor !== activeHash) {
      setSelection({
        selected: selection.selected.size ? selection.selected : new Set([activeHash]),
        anchor: activeHash,
        cursor: activeHash,
      });
    }
  }

  // Handlers read the latest list through refs, so they stay stable and
  // memoised rows don't re-render because of them.
  const orderRef = useRef(order);
  const detailOpenRef = useRef(activeHash !== undefined);
  useLayoutEffect(() => {
    orderRef.current = order;
    detailOpenRef.current = activeHash !== undefined;
  });
  const openDetail = useCallback(
    (hash: string) =>
      void navigate({
        to: "/torrents/$hash",
        params: { hash },
        search: (prev: ListSearch) => prev,
        // Switching between torrents doesn't fill the history.
        replace: detailOpenRef.current,
      }),
    [navigate],
  );
  const closeDetail = useCallback(
    () => void navigate({ to: "/", search: (prev: ListSearch) => prev }),
    [navigate],
  );

  const handlers = useMemo<RowHandlers>(
    () => ({
      onClick: (hash: string, e: MouseEvent) => {
        const toggle = e.metaKey || e.ctrlKey;
        setSelection((s) => clickSelect(s, hash, orderRef.current, { range: e.shiftKey, toggle }));
        if (!toggle && !e.shiftKey) {
          openDetail(hash);
        }
      },
      onToggle: (hash: string, range: boolean) =>
        setSelection((s) => clickSelect(s, hash, orderRef.current, { toggle: true, range })),
      onOpen: openDetail,
      onContextMenu: (hash: string) =>
        setSelection((s) => (s.selected.has(hash) ? s : clickSelect(s, hash, orderRef.current))),
    }),
    [openDetail],
  );

  const [removeOpen, setRemoveOpen] = useState(false);
  const [newTagOpen, setNewTagOpen] = useState(false);
  const action = useTorrentAction();
  const searchRef = useRef<HTMLInputElement>(null);

  const move = (delta: number, extend: boolean) => {
    const next = moveCursor(selection, order, delta, extend);
    setSelection(next);
    if (!extend && next.cursor && activeHash) {
      openDetail(next.cursor);
    }
  };
  useHotkeys({
    "/": () => searchRef.current?.focus(),
    ArrowDown: (e) => move(1, e.shiftKey),
    ArrowUp: (e) => move(-1, e.shiftKey),
    j: () => move(1, false),
    k: () => move(-1, false),
    Enter: () => selection.cursor && openDetail(selection.cursor),
    Space: () => {
      if (selectedTorrents.length > 0) {
        action.mutate({
          action: shouldStart(selectedTorrents) ? "start" : "stop",
          hashes: selectedHashes,
        });
      }
    },
    Delete: () => selectedTorrents.length > 0 && setRemoveOpen(true),
    Backspace: () => selectedTorrents.length > 0 && setRemoveOpen(true),
    "mod+a": () => setSelection((s) => selectAll(s, order)),
    Escape: () => {
      if (activeHash) {
        closeDetail();
      } else if (selection.selected.size > 0) {
        setSelection(emptySelection);
      }
    },
  });

  const toggleAll = () =>
    setSelection((s) =>
      allSelected === true ? { ...s, selected: new Set() } : selectAll(s, order),
    );

  const listProps = {
    rows,
    selected: selection.selected,
    activeHash,
    cursor: selection.cursor,
    handlers,
    allSelected,
    onToggleAll: toggleAll,
  };

  let body;
  if (!torrents) {
    body = (
      <div className="flex flex-1 items-center justify-center gap-3 text-muted-foreground">
        <Spinner /> Loading torrents…
      </div>
    );
  } else if (torrents.size === 0) {
    body = (
      <Empty className="flex-1">
        <EmptyHeader>
          <EmptyTitle className="font-heading text-2xl normal-case">No torrents yet</EmptyTitle>
          <EmptyDescription>
            Add a magnet link, a .torrent file or a URL. You can also paste a magnet link or drop
            .torrent files anywhere on this page.
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button size="sm" onClick={() => openAddDialog()}>
            <PlusIcon data-icon="inline-start" />
            Add torrents
          </Button>
        </EmptyContent>
      </Empty>
    );
  } else if (rows.length === 0) {
    body = (
      <Empty className="flex-1">
        <EmptyHeader>
          <EmptyTitle className="font-heading text-2xl normal-case">Nothing matches</EmptyTitle>
          <EmptyDescription>No torrent matches the filters and search.</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setQuery("");
              void navigate({ to: "/", search: {} });
            }}
          >
            <FilterXIcon data-icon="inline-start" />
            Clear filters
          </Button>
        </EmptyContent>
      </Empty>
    );
  } else {
    body = (
      <ContextMenu>
        <ContextMenuTrigger className="flex min-h-0 flex-1 flex-col select-auto">
          {isMobile ? (
            <TorrentCards {...listProps} />
          ) : (
            <TorrentTable
              {...listProps}
              sorting={sorting}
              onSortingChange={applyUpdater(setSorting)}
              columnVisibility={columnVisibility}
              onColumnVisibilityChange={applyUpdater(setColumnVisibility)}
              columnSizing={columnSizing}
              onColumnSizingChange={applyUpdater(setColumnSizing)}
            />
          )}
        </ContextMenuTrigger>
        <ContextMenuContent className="min-w-56">
          {selectedTorrents.length > 0 ? (
            <TorrentActionItems
              kind="context"
              torrents={selectedTorrents}
              onRemove={() => setRemoveOpen(true)}
              onNewTag={() => setNewTagOpen(true)}
            />
          ) : (
            <ContextMenuItem onClick={() => openAddDialog()}>
              <PlusIcon />
              Add torrents…
            </ContextMenuItem>
          )}
        </ContextMenuContent>
      </ContextMenu>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
        <InputGroup className="h-8 w-full max-w-sm sm:w-64 lg:w-80">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            ref={searchRef}
            type="text"
            role="searchbox"
            enterKeyHint="search"
            placeholder="Search torrents"
            aria-label="Search torrents"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                setQuery("");
                e.currentTarget.blur();
              }
            }}
          />
          <InputGroupAddon align="inline-end">
            {query ? (
              <InputGroupButton
                size="icon-xs"
                aria-label="Clear search"
                onClick={() => setQuery("")}
              >
                <XIcon />
              </InputGroupButton>
            ) : (
              <Kbd className="hidden sm:inline-flex">/</Kbd>
            )}
          </InputGroupAddon>
        </InputGroup>
        <FilterChips search={search} />
        <div className="flex-1" />
        {selectedTorrents.length > 0 ? (
          <div className="flex items-center gap-1" role="toolbar" aria-label="Selected torrents">
            <span className="mr-1 text-xs text-muted-foreground tabular">
              {plural(selectedTorrents.length, "selected", "selected")}
            </span>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Start"
              title="Start"
              onClick={() => action.mutate({ action: "start", hashes: selectedHashes })}
            >
              <PlayIcon />
            </Button>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Stop"
              title="Stop"
              onClick={() => action.mutate({ action: "stop", hashes: selectedHashes })}
            >
              <PauseIcon />
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label="More actions"
                    title="More actions"
                  />
                }
              >
                <EllipsisIcon />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto min-w-56">
                <TorrentActionItems
                  kind="dropdown"
                  compact
                  torrents={selectedTorrents}
                  onRemove={() => setRemoveOpen(true)}
                  onNewTag={() => setNewTagOpen(true)}
                />
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Clear selection"
              title="Clear selection"
              onClick={() => setSelection(emptySelection)}
            >
              <XIcon />
            </Button>
          </div>
        ) : (
          <span className="hidden text-xs text-muted-foreground tabular sm:inline">
            {torrents && rows.length !== torrents.size
              ? `${rows.length.toLocaleString()} of ${plural(torrents.size, "torrent")}`
              : plural(rows.length, "torrent")}
          </span>
        )}
        {!isMobile && (
          <ColumnChooser
            visibility={columnVisibility}
            onChange={setColumnVisibility}
            onResetSizes={() => setColumnSizing({})}
          />
        )}
        <Button size="sm" onClick={() => openAddDialog()} className="max-sm:hidden">
          <PlusIcon data-icon="inline-start" />
          Add
        </Button>
        <Button
          size="icon-sm"
          onClick={() => openAddDialog()}
          className="sm:hidden"
          aria-label="Add torrents"
        >
          <PlusIcon />
        </Button>
      </div>
      <DetailSplit open={activeHash !== undefined} detail={<Outlet />}>
        {body}
      </DetailSplit>
      <RemoveDialog
        torrents={selectedTorrents}
        open={removeOpen}
        onOpenChange={setRemoveOpen}
        onRemoved={() => {
          setSelection(emptySelection);
          if (activeHash && selectedHashes.includes(activeHash)) {
            closeDetail();
          }
        }}
      />
      <NewTagDialog hashes={selectedHashes} open={newTagOpen} onOpenChange={setNewTagOpen} />
    </div>
  );
}

function FilterChips({ search }: { search: ListSearch }) {
  const chips: { key: keyof ListSearch; label: string }[] = [];
  if (search.filter) {
    chips.push({ key: "filter", label: stateFilterLabels[search.filter] });
  }
  if (search.category !== undefined) {
    chips.push({ key: "category", label: search.category || "Uncategorized" });
  }
  if (search.tag !== undefined) {
    chips.push({ key: "tag", label: search.tag ? `#${search.tag}` : "Untagged" });
  }
  if (search.tracker !== undefined) {
    chips.push({ key: "tracker", label: search.tracker || "Trackerless" });
  }
  if (chips.length === 0) {
    return null;
  }
  return (
    <ul className="flex flex-wrap items-center gap-1" aria-label="Active filters">
      {chips.map((c) => (
        <li key={c.key}>
          <Link
            to="."
            search={(prev: ListSearch) => ({ ...prev, [c.key]: undefined })}
            className="flex h-7 items-center gap-1 bg-secondary px-2 text-xs hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none"
            aria-label={`Remove filter ${c.label}`}
          >
            {c.label}
            <XIcon className="size-3" />
          </Link>
        </li>
      ))}
    </ul>
  );
}

function ColumnChooser({
  visibility,
  onChange,
  onResetSizes,
}: {
  visibility: ColumnVisibilityState;
  onChange: (v: ColumnVisibilityState) => void;
  onResetSizes: () => void;
}) {
  const choosable = columns.filter((c) => c.enableHiding !== false);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button size="icon-sm" variant="ghost" aria-label="Columns" title="Columns" />}
      >
        <Columns3Icon />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-96 w-auto min-w-52">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Columns</DropdownMenuLabel>
          {choosable.map((c) => {
            const id = c.id ?? (c as { accessorKey?: string }).accessorKey ?? "";
            return (
              <DropdownMenuCheckboxItem
                key={id}
                checked={visibility[id] !== false}
                closeOnClick={false}
                onCheckedChange={(checked) => onChange({ ...visibility, [id]: checked })}
              >
                {c.meta?.label ?? id}
              </DropdownMenuCheckboxItem>
            );
          })}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => {
            onChange(defaultVisibility);
            onResetSizes();
          }}
        >
          Reset columns
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
