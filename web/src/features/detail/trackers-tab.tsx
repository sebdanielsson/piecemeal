import { useQuery } from "@tanstack/react-query";
import { PencilIcon } from "lucide-react";
import { useState } from "react";
import { api } from "@/api/client";
import type { Torrent, Tracker } from "@/api/types";
import { DataTable, type DataColumn } from "@/components/data/data-table";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { unwrap } from "@/lib/api";
import { trackerHost } from "@/lib/format";
import { keys } from "@/lib/queries";
import { EditTrackersDialog } from "./edit-trackers-dialog";

const columns: DataColumn<Tracker>[] = [
  { id: "tier", header: "Tier", cell: (t) => t.tier + 1, sort: (t) => t.tier, className: "w-16" },
  { id: "host", header: "Host", cell: (t) => trackerHost(t.url), sort: (t) => trackerHost(t.url) },
  {
    id: "url",
    header: "URL",
    cell: (t) => (
      <span className="block max-w-[40rem] truncate font-mono text-xs" title={t.url}>
        {t.url}
      </span>
    ),
    sort: (t) => t.url,
  },
];

export function TrackersTab({ torrent }: { torrent: Torrent }) {
  const hash = torrent.hash;
  const trackers = useQuery({
    queryKey: keys.trackers(hash),
    queryFn: async () =>
      (await unwrap(api.GET("/torrents/{hash}/trackers", { params: { path: { hash } } }))).items ??
      [],
    refetchInterval: 30_000,
  });
  const webSeeds = useQuery({
    queryKey: keys.webSeeds(hash),
    queryFn: async () =>
      (await unwrap(api.GET("/torrents/{hash}/web-seeds", { params: { path: { hash } } }))).urls ??
      [],
  });
  const [editing, setEditing] = useState(false);
  if (trackers.isPending) {
    return (
      <div className="flex justify-center p-6">
        <Spinner />
      </div>
    );
  }
  if (trackers.isError) {
    return (
      <p className="p-4 text-destructive">Couldn't load the trackers: {trackers.error.message}</p>
    );
  }
  const seeds = webSeeds.data ?? [];
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b px-3 py-1.5 text-xs text-muted-foreground">
        <span className="tabular">
          {seeds.length > 0 && `${seeds.length} web ${seeds.length === 1 ? "seed" : "seeds"}`}
        </span>
        <div className="flex-1" />
        <Button
          size="xs"
          variant="ghost"
          // Saving sends the web seeds too, so wait for them.
          disabled={!webSeeds.isSuccess}
          onClick={() => setEditing(true)}
        >
          <PencilIcon data-icon="inline-start" />
          Edit
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          rows={trackers.data}
          columns={columns}
          rowKey={(t) => `${t.tier}:${t.url}`}
          initialSort={{ id: "tier", desc: false }}
          empty={
            torrent.private
              ? "No trackers."
              : "No trackers. Peers come from the DHT and peer exchange."
          }
          label="Trackers"
        />
        {seeds.length > 0 && (
          <ul className="border-t px-3 py-2 font-mono text-xs" aria-label="Web seeds">
            {seeds.map((url) => (
              <li key={url} className="truncate" title={url}>
                {url}
              </li>
            ))}
          </ul>
        )}
      </div>
      <EditTrackersDialog
        hash={hash}
        trackers={trackers.data}
        webSeeds={seeds}
        open={editing}
        onOpenChange={setEditing}
      />
    </div>
  );
}
