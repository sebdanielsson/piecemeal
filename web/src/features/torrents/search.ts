import { isStateFilter, type ListFilter, type StateFilter } from "@/lib/filters";

/**
 * The torrent list's URL search parameters. For category, tag and tracker,
 * an empty string selects torrents without one.
 */
export interface ListSearch {
  filter?: StateFilter;
  category?: string;
  tag?: string;
  tracker?: string;
  q?: string;
}

const str = (v: unknown): string | undefined =>
  typeof v === "string" ? v : typeof v === "number" ? String(v) : undefined;

export function parseListSearch(search: Record<string, unknown>): ListSearch {
  const out: ListSearch = {};
  if (isStateFilter(search.filter) && search.filter !== "all") {
    out.filter = search.filter;
  }
  for (const key of ["category", "tag", "tracker"] as const) {
    const v = str(search[key]);
    if (v !== undefined) {
      out[key] = v;
    }
  }
  const q = str(search.q);
  if (q) {
    out.q = q;
  }
  return out;
}

export function listFilterFrom(search: ListSearch): ListFilter {
  return {
    state: search.filter ?? "all",
    category: search.category,
    tag: search.tag,
    tracker: search.tracker,
    search: search.q,
  };
}
