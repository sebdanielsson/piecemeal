// Pure functions that apply the event stream to client-side state. The
// stream (stream.ts) writes their results into the TanStack Query cache.

import type { Torrent, TorrentsEvent, TransferStatus } from "@/api/types";

/** Every torrent by infohash, in the order the server first reported them. */
export type TorrentMap = ReadonlyMap<string, Torrent>;

export function torrentMapFrom(list: readonly Torrent[] | null | undefined): TorrentMap {
  const map = new Map<string, Torrent>();
  for (const t of list ?? []) {
    map.set(t.hash, t);
  }
  return map;
}

/**
 * Applies a torrents event. Torrents the event doesn't mention keep their
 * object identity, so memoised rows skip re-rendering. Returns the previous
 * map when nothing changed.
 */
export function applyTorrentsEvent(prev: TorrentMap | undefined, ev: TorrentsEvent): TorrentMap {
  const updated = ev.updated ?? [];
  const removed = ev.removed ?? [];
  const base = prev ?? new Map<string, Torrent>();
  if (updated.length === 0 && !removed.some((h) => base.has(h))) {
    return base;
  }
  const next = new Map(base);
  for (const t of updated) {
    next.set(t.hash, t);
  }
  for (const h of removed) {
    next.delete(h);
  }
  return next;
}

/** Replaces or inserts one torrent, as returned by a mutation. */
export function upsertTorrent(prev: TorrentMap | undefined, t: Torrent): TorrentMap {
  const next = new Map(prev ?? []);
  next.set(t.hash, t);
  return next;
}

/** Removes torrents, such as after a delete, without waiting for the stream. */
export function removeTorrents(
  prev: TorrentMap | undefined,
  hashes: readonly string[],
): TorrentMap {
  if (!prev || !hashes.some((h) => prev.has(h))) {
    return prev ?? new Map();
  }
  const next = new Map(prev);
  for (const h of hashes) {
    next.delete(h);
  }
  return next;
}

/** One point of the transfer graph. */
export interface TransferSample {
  /** Milliseconds since the epoch. */
  at: number;
  down: number;
  up: number;
}

/** How many samples the transfer graph keeps: 15 minutes at one per second. */
export const transferHistoryLimit = 900;

/**
 * Appends a transfer sample, dropping the oldest beyond the limit. Samples
 * closer than half a second to the previous one replace it, so bursts don't
 * crowd the graph.
 */
export function appendTransferSample(
  prev: readonly TransferSample[] | undefined,
  transfer: Pick<TransferStatus, "downloadRate" | "uploadRate">,
  at: number,
  limit = transferHistoryLimit,
): TransferSample[] {
  const sample = { at, down: transfer.downloadRate, up: transfer.uploadRate };
  const list = prev ? [...prev] : [];
  const last = list.at(-1);
  if (last && at - last.at < 500) {
    list[list.length - 1] = sample;
  } else {
    list.push(sample);
  }
  return list.length > limit ? list.slice(list.length - limit) : list;
}
