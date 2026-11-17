import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { Torrent, Tracker } from "@/api/types";
import { DataTable, type DataColumn } from "@/components/data/data-table";
import { Spinner } from "@/components/ui/spinner";
import { unwrap } from "@/lib/api";
import { trackerHost } from "@/lib/format";
import { keys } from "@/lib/queries";

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
  return (
    <DataTable
      rows={trackers.data}
      columns={columns}
      rowKey={(t) => `${t.tier}:${t.url}`}
      initialSort={{ id: "tier", desc: false }}
      empty={
        torrent.private ? "No trackers." : "No trackers. Peers come from the DHT and peer exchange."
      }
      label="Trackers"
    />
  );
}
