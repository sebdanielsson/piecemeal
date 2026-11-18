import { useNavigate, useParams } from "@tanstack/react-router";
import {
  CheckIcon,
  PauseIcon,
  PencilIcon,
  PlayIcon,
  RefreshCwIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import { lazy, Suspense, useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { usePersistentState } from "@/hooks/use-persistent-state";
import { formatPercent } from "@/lib/format";
import { useTorrent } from "@/lib/live/hooks";
import { useTorrentAction, useUpdateTorrent } from "@/lib/mutations";
import type { ListSearch } from "../torrents/search";
import { RemoveDialog } from "../torrents/remove-dialog";
import { ProgressBar, StateBadge } from "../torrents/torrent-bits";
import { GeneralTab } from "./general-tab";

const FilesTab = lazy(() => import("./files-tab").then((m) => ({ default: m.FilesTab })));
const PeersTab = lazy(() => import("./peers-tab").then((m) => ({ default: m.PeersTab })));
const TrackersTab = lazy(() => import("./trackers-tab").then((m) => ({ default: m.TrackersTab })));
const PiecesTab = lazy(() => import("./pieces-tab").then((m) => ({ default: m.PiecesTab })));

const tabs = ["general", "files", "peers", "trackers", "pieces"] as const;
type Tab = (typeof tabs)[number];

export function TorrentDetail() {
  const { hash } = useParams({ from: "/app/list/torrents/$hash" });
  const { torrent, loaded } = useTorrent(hash);
  const navigate = useNavigate();
  const close = () => void navigate({ to: "/", search: (prev: ListSearch) => prev });
  const [tab, setTab] = usePersistentState<Tab>("piecemeal.detail.tab", "general", (v): v is Tab =>
    (tabs as readonly unknown[]).includes(v),
  );
  const action = useTorrentAction();
  const [removeOpen, setRemoveOpen] = useState(false);

  if (!torrent) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-sm text-muted-foreground">
        {loaded ? (
          <>
            <p>This torrent isn't here any more.</p>
            <Button size="sm" variant="outline" onClick={close}>
              Close
            </Button>
          </>
        ) : (
          <Spinner />
        )}
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex items-start gap-2 border-b px-3 pt-2 pb-2 sm:px-4">
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          {/* The router keeps this component when only the hash changes; the
              key starts the name over, so a rename can't land on another torrent. */}
          <TorrentName key={torrent.hash} hash={torrent.hash} name={torrent.name} />
          <div className="flex items-center gap-3 text-xs">
            <StateBadge torrent={torrent} className="shrink-0" />
            <ProgressBar torrent={torrent} showLabel={false} className="max-w-48" />
            <span className="text-muted-foreground tabular">{formatPercent(torrent.progress)}</span>
          </div>
        </div>
        <div className="flex shrink-0 items-center">
          {torrent.stopped ? (
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Start"
              title="Start"
              onClick={() => action.mutate({ action: "start", hashes: [torrent.hash] })}
            >
              <PlayIcon />
            </Button>
          ) : (
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Stop"
              title="Stop"
              onClick={() => action.mutate({ action: "stop", hashes: [torrent.hash] })}
            >
              <PauseIcon />
            </Button>
          )}
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="Recheck"
            title="Recheck"
            onClick={() => action.mutate({ action: "recheck", hashes: [torrent.hash] })}
          >
            <RefreshCwIcon />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="Remove"
            title="Remove"
            onClick={() => setRemoveOpen(true)}
          >
            <Trash2Icon />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="Close details"
            title="Close (Esc)"
            onClick={close}
          >
            <XIcon />
          </Button>
        </div>
      </header>
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="min-h-0 flex-1 gap-0">
        <TabsList
          variant="line"
          className="h-9 w-full justify-start overflow-x-auto border-b px-2 group-data-horizontal/tabs:h-9"
        >
          <TabsTrigger value="general" className="flex-none">
            General
          </TabsTrigger>
          <TabsTrigger value="files" className="flex-none">
            Files
          </TabsTrigger>
          <TabsTrigger value="peers" className="flex-none">
            Peers
            <span className="text-muted-foreground tabular">
              {torrent.seeds + torrent.peers || ""}
            </span>
          </TabsTrigger>
          <TabsTrigger value="trackers" className="flex-none">
            Trackers
          </TabsTrigger>
          <TabsTrigger value="pieces" className="flex-none">
            Pieces
          </TabsTrigger>
        </TabsList>
        <div className="min-h-0 flex-1 overflow-auto">
          <Suspense
            fallback={
              <div className="flex justify-center p-6">
                <Spinner />
              </div>
            }
          >
            <TabsContent value="general">
              <GeneralTab torrent={torrent} />
            </TabsContent>
            <TabsContent value="files" className="h-full">
              <FilesTab key={torrent.hash} torrent={torrent} />
            </TabsContent>
            <TabsContent value="peers">
              <PeersTab key={torrent.hash} torrent={torrent} />
            </TabsContent>
            <TabsContent value="trackers">
              <TrackersTab torrent={torrent} />
            </TabsContent>
            <TabsContent value="pieces">
              <PiecesTab torrent={torrent} />
            </TabsContent>
          </Suspense>
        </div>
      </Tabs>
      <RemoveDialog
        torrents={[torrent]}
        open={removeOpen}
        onOpenChange={setRemoveOpen}
        onRemoved={close}
      />
    </div>
  );
}

function TorrentName({ hash, name }: { hash: string; name: string }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(name);
  const update = useUpdateTorrent(hash);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const next = draft.trim();
    if (!next || next === name) {
      setEditing(false);
      return;
    }
    update.mutate({ name: next }, { onSuccess: () => setEditing(false) });
  };
  if (editing) {
    return (
      <form onSubmit={submit} className="flex items-center gap-1">
        <Input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          aria-label="Name"
          autoFocus
          className="h-8 text-sm"
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.stopPropagation();
              setEditing(false);
            }
          }}
        />
        <Button
          type="submit"
          size="icon-sm"
          variant="ghost"
          aria-label="Save name"
          disabled={update.isPending}
        >
          <CheckIcon />
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label="Cancel"
          onClick={() => setEditing(false)}
        >
          <XIcon />
        </Button>
      </form>
    );
  }
  return (
    <div className="group/name flex min-w-0 items-center gap-1">
      <h2 className="truncate text-base font-semibold" title={name}>
        {name}
      </h2>
      <Button
        size="icon-xs"
        variant="ghost"
        aria-label="Rename"
        title="Rename"
        className="shrink-0 opacity-60 group-hover/name:opacity-100"
        onClick={() => {
          setDraft(name);
          setEditing(true);
        }}
      >
        <PencilIcon />
      </Button>
    </div>
  );
}
