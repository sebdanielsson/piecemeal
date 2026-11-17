import { useQuery } from "@tanstack/react-query";
import { useCallback } from "react";
import type { Torrent } from "@/api/types";
import { torrentsQuery, transferHistoryQuery, transferQuery } from "../queries";
import type { TorrentMap } from "./reducer";

/** Every torrent, kept current by the event stream. */
export function useTorrentMap(): TorrentMap | undefined {
  return useQuery(torrentsQuery).data;
}

/** One torrent from the live list; re-renders only when that torrent changes. */
export function useTorrent(hash: string): { torrent: Torrent | undefined; loaded: boolean } {
  const select = useCallback((m: TorrentMap) => m.get(hash), [hash]);
  const q = useQuery({ ...torrentsQuery, select });
  return { torrent: q.data, loaded: q.isSuccess };
}

export function useTransfer() {
  return useQuery(transferQuery).data;
}

export function useTransferHistory() {
  return useQuery(transferHistoryQuery).data ?? [];
}
