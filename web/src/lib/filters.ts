// Client-side filtering of the live torrent list. The state filters mirror
// the server's (internal/api MatchesFilter), so the sidebar counts match what
// GET /torrents?filter= would return.

import type { Torrent, TorrentState } from "@/api/types";
import { trackerHost } from "./format";

export const stateFilters = [
  "all",
  "downloading",
  "seeding",
  "completed",
  "stopped",
  "active",
  "inactive",
  "stalled",
  "checking",
  "errored",
] as const;

export type StateFilter = (typeof stateFilters)[number];

export const stateFilterLabels: Record<StateFilter, string> = {
  all: "All",
  downloading: "Downloading",
  seeding: "Seeding",
  completed: "Completed",
  stopped: "Stopped",
  active: "Active",
  inactive: "Inactive",
  stalled: "Stalled",
  checking: "Checking",
  errored: "Errored",
};

export function isStateFilter(value: unknown): value is StateFilter {
  return typeof value === "string" && (stateFilters as readonly string[]).includes(value);
}

export function matchesState(t: Torrent, filter: StateFilter): boolean {
  switch (filter) {
    case "downloading":
      return !t.complete && !t.stopped && t.state !== "error";
    case "seeding":
      return t.state === "seeding" || t.state === "stalledSeeding";
    case "completed":
      return t.complete;
    case "stopped":
      return t.stopped;
    case "active":
      return t.downloadRate > 0 || t.uploadRate > 0;
    case "inactive":
      return t.downloadRate === 0 && t.uploadRate === 0;
    case "stalled":
      return t.state === "stalledDownloading" || t.state === "stalledSeeding";
    case "checking":
      return t.state === "checking";
    case "errored":
      return t.state === "error";
    default:
      return true;
  }
}

/**
 * What the list shows. For category, tag and tracker, undefined means any
 * and "" means none (uncategorized, untagged, no tracker).
 */
export interface ListFilter {
  state: StateFilter;
  category?: string;
  tag?: string;
  tracker?: string;
  search?: string;
}

// Parsing tracker URLs for every torrent on every update adds up, and a
// client has few distinct trackers.
const hostCache = new Map<string, string>();

export function torrentTrackerHost(t: Torrent): string {
  let host = hostCache.get(t.tracker);
  if (host === undefined) {
    host = trackerHost(t.tracker);
    if (hostCache.size > 10_000) {
      hostCache.clear();
    }
    hostCache.set(t.tracker, host);
  }
  return host;
}

export function matchesFilter(
  t: Torrent,
  f: ListFilter,
  search = normaliseSearch(f.search),
): boolean {
  if (!matchesState(t, f.state)) {
    return false;
  }
  if (f.category !== undefined && t.category !== f.category) {
    return false;
  }
  if (f.tag !== undefined) {
    const tags = t.tags ?? [];
    if (f.tag === "" ? tags.length > 0 : !tags.includes(f.tag)) {
      return false;
    }
  }
  if (f.tracker !== undefined && torrentTrackerHost(t) !== f.tracker) {
    return false;
  }
  if (search && !matchesSearch(t, search)) {
    return false;
  }
  return true;
}

function normaliseSearch(search: string | undefined): string {
  return (search ?? "").trim().toLowerCase();
}

/** Matches the name, ignoring case, or an infohash prefix. */
function matchesSearch(t: Torrent, search: string): boolean {
  return t.name.toLowerCase().includes(search) || t.hash.startsWith(search);
}

export function filterTorrents(torrents: Iterable<Torrent>, f: ListFilter): Torrent[] {
  const search = normaliseSearch(f.search);
  const out: Torrent[] = [];
  for (const t of torrents) {
    if (matchesFilter(t, f, search)) {
      out.push(t);
    }
  }
  return out;
}

export interface FilterCounts {
  total: number;
  states: Record<StateFilter, number>;
  /** By category; "" counts torrents without one. */
  categories: Map<string, number>;
  /** By tag; "" counts torrents without tags. */
  tags: Map<string, number>;
  /** By tracker host; "" counts torrents without a tracker. */
  trackers: Map<string, number>;
}

function bump(m: Map<string, number>, k: string) {
  m.set(k, (m.get(k) ?? 0) + 1);
}

/** Counts torrents per sidebar entry in one pass. */
export function countTorrents(torrents: Iterable<Torrent>): FilterCounts {
  const states = Object.fromEntries(stateFilters.map((f) => [f, 0])) as Record<StateFilter, number>;
  const categories = new Map<string, number>();
  const tags = new Map<string, number>();
  const trackers = new Map<string, number>();
  let total = 0;
  for (const t of torrents) {
    total++;
    for (const f of stateFilters) {
      if (matchesState(t, f)) {
        states[f]++;
      }
    }
    bump(categories, t.category);
    const list = t.tags ?? [];
    if (list.length === 0) {
      bump(tags, "");
    }
    for (const tag of list) {
      bump(tags, tag);
    }
    bump(trackers, torrentTrackerHost(t));
  }
  return { total, states, categories, tags, trackers };
}

export interface StateInfo {
  label: string;
  tone: "progress" | "success" | "muted" | "warning" | "danger" | "info";
}

/** The label and colour of a torrent's state badge. */
export function describeState(t: Pick<Torrent, "state" | "complete" | "forced">): StateInfo {
  const forced = t.forced ? " (forced)" : "";
  const map: Record<TorrentState, StateInfo> = {
    metadata: { label: "Metadata", tone: "info" },
    checking: { label: "Checking", tone: "info" },
    downloading: { label: `Downloading${forced}`, tone: "progress" },
    stalledDownloading: { label: "Stalled", tone: "warning" },
    seeding: { label: `Seeding${forced}`, tone: "success" },
    stalledSeeding: { label: "Seeding (idle)", tone: "success" },
    stopped: { label: t.complete ? "Completed" : "Stopped", tone: "muted" },
    queued: { label: "Queued", tone: "muted" },
    moving: { label: "Moving", tone: "info" },
    error: { label: "Error", tone: "danger" },
  };
  return map[t.state];
}
