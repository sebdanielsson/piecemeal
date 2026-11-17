import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LockIcon, PlusIcon } from "lucide-react";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { Peer, Torrent } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { errorMessage, unwrap } from "@/lib/api";
import { formatActiveRate, formatBytes, formatPercent } from "@/lib/format";
import { keys } from "@/lib/queries";
import { DataTable, type DataColumn } from "@/components/data/data-table";

const columns: DataColumn<Peer>[] = [
  {
    id: "address",
    header: "Address",
    cell: (p) => <span className="font-mono text-xs">{p.address}</span>,
    sort: (p) => p.address,
  },
  { id: "client", header: "Client", cell: (p) => p.client || "–", sort: (p) => p.client },
  {
    id: "network",
    header: "Connection",
    cell: (p) => (
      <span className="flex items-center gap-1">
        {p.network}
        {p.encryption !== "none" && (
          <LockIcon
            className="size-3 text-muted-foreground"
            aria-label={`Encrypted (${p.encryption})`}
          />
        )}
      </span>
    ),
    sort: (p) => p.network,
  },
  { id: "source", header: "Source", cell: (p) => p.source, sort: (p) => p.source },
  {
    id: "progress",
    header: "Progress",
    align: "right",
    cell: (p) => formatPercent(p.progress),
    sort: (p) => p.progress,
  },
  {
    id: "down",
    header: "Down",
    align: "right",
    cell: (p) => formatActiveRate(p.downloadRate),
    sort: (p) => p.downloadRate,
  },
  {
    id: "up",
    header: "Up",
    align: "right",
    cell: (p) => formatActiveRate(p.uploadRate),
    sort: (p) => p.uploadRate,
  },
  {
    id: "downloaded",
    header: "Downloaded",
    align: "right",
    cell: (p) => formatBytes(p.downloaded),
    sort: (p) => p.downloaded,
  },
  {
    id: "uploaded",
    header: "Uploaded",
    align: "right",
    cell: (p) => formatBytes(p.uploaded),
    sort: (p) => p.uploaded,
  },
];

export function PeersTab({ torrent }: { torrent: Torrent }) {
  const hash = torrent.hash;
  const peers = useQuery({
    queryKey: keys.peers(hash),
    queryFn: async () =>
      (await unwrap(api.GET("/torrents/{hash}/peers", { params: { path: { hash } } }))).items ?? [],
    refetchInterval: 2_000,
  });
  const [adding, setAdding] = useState(false);

  if (peers.isPending) {
    return (
      <div className="flex justify-center p-6">
        <Spinner />
      </div>
    );
  }
  if (peers.isError) {
    return <p className="p-4 text-destructive">Couldn't load the peers: {peers.error.message}</p>;
  }
  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 border-b px-3 py-1.5 text-xs text-muted-foreground">
        <span className="tabular">{peers.data.length.toLocaleString()} connected</span>
        <div className="flex-1" />
        {adding ? (
          <AddPeersForm hash={hash} onDone={() => setAdding(false)} />
        ) : (
          <Button size="xs" variant="ghost" onClick={() => setAdding(true)}>
            <PlusIcon data-icon="inline-start" />
            Add peers
          </Button>
        )}
      </div>
      <DataTable
        rows={peers.data}
        columns={columns}
        rowKey={(p) => p.address}
        initialSort={{ id: "down", desc: true }}
        empty={torrent.stopped ? "The torrent is stopped." : "No peers connected."}
        label="Peers"
      />
    </div>
  );
}

function AddPeersForm({ hash, onDone }: { hash: string; onDone: () => void }) {
  const [text, setText] = useState("");
  const qc = useQueryClient();
  const add = useMutation({
    mutationFn: (peers: string[]) =>
      unwrap(
        api.POST("/torrents/{hash}/peers", {
          params: { path: { hash } },
          body: { addresses: peers },
        }),
      ),
    onSuccess: () => {
      toast.success("Connecting to the peers");
      void qc.invalidateQueries({ queryKey: keys.peers(hash) });
      onDone();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const peers = text.split(/[\s,]+/).filter(Boolean);
    if (peers.length > 0) {
      add.mutate(peers);
    }
  };
  return (
    <form onSubmit={submit} className="flex items-center gap-1">
      <Input
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="ip:port, …"
        aria-label="Peers to add"
        className="h-7 w-56 text-xs"
        autoFocus
      />
      <Button type="submit" size="xs" disabled={add.isPending || !text.trim()}>
        Add
      </Button>
      <Button type="button" size="xs" variant="ghost" onClick={onDone}>
        Cancel
      </Button>
    </form>
  );
}
