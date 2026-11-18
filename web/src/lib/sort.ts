// Sorting the live torrent list. Every comparator breaks ties by infohash, so
// rows don't jump around when values are equal.

import type { Torrent } from "@/api/types";
import { torrentTrackerHost } from "./filters";

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

type Compare = (a: Torrent, b: Torrent) => number;

const num =
  (pick: (t: Torrent) => number): Compare =>
  (a, b) =>
    pick(a) - pick(b);

const text =
  (pick: (t: Torrent) => string): Compare =>
  (a, b) =>
    collator.compare(pick(a), pick(b));

// Unknown ETAs (-1) and finished ones (0) sort after every real estimate.
const etaKey = (t: Torrent) => (t.eta > 0 ? t.eta : Number.MAX_SAFE_INTEGER);

// Sorted by how far along a torrent is, then by what it is doing.
const stateOrder: Record<Torrent["state"], number> = {
  downloading: 0,
  stalledDownloading: 1,
  metadata: 2,
  checking: 3,
  queuedForChecking: 4,
  moving: 5,
  queued: 6,
  seeding: 7,
  stalledSeeding: 8,
  stopped: 9,
  error: 10,
};

export const torrentComparators = {
  name: text((t) => t.name),
  size: num((t) => t.size),
  totalSize: num((t) => t.totalSize),
  progress: num((t) => t.progress),
  state: num((t) => stateOrder[t.state]),
  downloadRate: num((t) => t.downloadRate),
  uploadRate: num((t) => t.uploadRate),
  eta: num(etaKey),
  ratio: num((t) => t.ratio),
  seeds: num((t) => t.seeds),
  peers: num((t) => t.peers),
  category: text((t) => t.category),
  tags: text((t) => (t.tags ?? []).join(", ")),
  addedAt: num((t) => t.addedAt),
  completedAt: num((t) => t.completedAt),
  downloaded: num((t) => t.downloaded),
  uploaded: num((t) => t.uploaded),
  tracker: text(torrentTrackerHost),
  savePath: text((t) => t.savePath),
  seedingTime: num((t) => t.seedingTime),
  activeTime: num((t) => t.activeTime),
  lastActivityAt: num((t) => t.lastActivityAt),
  remaining: num((t) => t.size - t.completed),
  queuePosition: num((t) => t.queuePosition),
} satisfies Record<string, Compare>;

export type SortKey = keyof typeof torrentComparators;

export interface SortSpec {
  id: string;
  desc: boolean;
}

export function isSortKey(id: string): id is SortKey {
  return Object.hasOwn(torrentComparators, id);
}

/** Returns a sorted copy. An unknown key keeps the input order. */
export function sortTorrents(list: readonly Torrent[], sort: SortSpec | undefined): Torrent[] {
  const out = [...list];
  if (!sort || !isSortKey(sort.id)) {
    return out;
  }
  const cmp = torrentComparators[sort.id];
  const dir = sort.desc ? -1 : 1;
  out.sort((a, b) => {
    const c = cmp(a, b);
    if (c !== 0) {
      return c * dir;
    }
    return a.hash < b.hash ? -1 : a.hash > b.hash ? 1 : 0;
  });
  return out;
}
