// Query keys and options. The torrent list and transfer status come only from
// the event stream (live/stream.ts), so their queries never fetch; everything
// else is fetched and, where the stream carries it, kept current by events.

import { queryOptions, skipToken, type QueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type {
  Category,
  Principal,
  RSSRule,
  RSSTree,
  SettingsResponse,
  TransferStatus,
} from "@/api/types";
import { ApiError, unwrap } from "./api";
import type { TorrentMap, TransferSample } from "./live/reducer";

export const keys = {
  me: ["auth", "me"] as const,
  authConfig: ["auth", "config"] as const,
  torrents: ["live", "torrents"] as const,
  transfer: ["live", "transfer"] as const,
  transferHistory: ["live", "transfer-history"] as const,
  categories: ["categories"] as const,
  tags: ["tags"] as const,
  settings: ["settings"] as const,
  system: ["system"] as const,
  torrent: (hash: string) => ["torrent", hash] as const,
  files: (hash: string) => ["torrent", hash, "files"] as const,
  peers: (hash: string) => ["torrent", hash, "peers"] as const,
  trackers: (hash: string) => ["torrent", hash, "trackers"] as const,
  webSeeds: (hash: string) => ["torrent", hash, "webSeeds"] as const,
  pieces: (hash: string) => ["torrent", hash, "pieces"] as const,
  apiKeys: ["api-keys"] as const,
  sessions: ["sessions"] as const,
  users: ["users"] as const,
  audit: ["audit"] as const,
  log: ["log"] as const,
  rss: ["rss"] as const,
  rssFeed: (uid: string) => ["rss", "feed", uid] as const,
  rssRules: ["rss", "rules"] as const,
  rssMatches: (name: string) => ["rss", "rules", name, "matches"] as const,
};

/** Who is signed in, or null. */
export const meQuery = queryOptions({
  queryKey: keys.me,
  queryFn: async (): Promise<Principal | null> => {
    try {
      return await unwrap(api.GET("/auth/me"));
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        return null;
      }
      throw error;
    }
  },
  staleTime: 5 * 60_000,
  retry: false,
});

export const authConfigQuery = queryOptions({
  queryKey: keys.authConfig,
  queryFn: () => unwrap(api.GET("/auth/config")),
  staleTime: Infinity,
});

// Filled by the event stream.
export const torrentsQuery = queryOptions<TorrentMap>({
  queryKey: keys.torrents,
  queryFn: skipToken,
  staleTime: Infinity,
  gcTime: Infinity,
  structuralSharing: false,
});

export const transferQuery = queryOptions<TransferStatus>({
  queryKey: keys.transfer,
  queryFn: skipToken,
  staleTime: Infinity,
  gcTime: Infinity,
  structuralSharing: false,
});

export const transferHistoryQuery = queryOptions<TransferSample[]>({
  queryKey: keys.transferHistory,
  queryFn: skipToken,
  staleTime: Infinity,
  gcTime: Infinity,
  structuralSharing: false,
});

export const categoriesQuery = queryOptions({
  queryKey: keys.categories,
  queryFn: async (): Promise<Category[]> => (await unwrap(api.GET("/categories"))).items ?? [],
  staleTime: Infinity,
});

export const tagsQuery = queryOptions({
  queryKey: keys.tags,
  queryFn: async (): Promise<string[]> => (await unwrap(api.GET("/tags"))).items ?? [],
  staleTime: Infinity,
});

export const settingsQuery = queryOptions({
  queryKey: keys.settings,
  queryFn: (): Promise<SettingsResponse> => unwrap(api.GET("/settings")),
  staleTime: Infinity,
});

/** RSS folders and feeds; polled faster while a feed is loading. */
export const rssQuery = queryOptions({
  queryKey: keys.rss,
  queryFn: (): Promise<RSSTree> => unwrap(api.GET("/rss")),
  refetchInterval: (q) => ((q.state.data?.feeds ?? []).some((f) => f.loading) ? 2_000 : 30_000),
});

export const rssRulesQuery = queryOptions({
  queryKey: keys.rssRules,
  queryFn: async (): Promise<Record<string, RSSRule>> =>
    (await unwrap(api.GET("/rss/rules"))).items ?? {},
});

export const systemQuery = queryOptions({
  queryKey: keys.system,
  queryFn: () => unwrap(api.GET("/system")),
  staleTime: 30_000,
  refetchInterval: 30_000,
});

/**
 * Drops everything cached for the signed-in user, after leaving the app for
 * the sign-in page. The sign-in methods stay, since that page shows them.
 */
export function forgetSession(qc: QueryClient): void {
  qc.setQueryData(keys.me, null);
  qc.removeQueries({ predicate: (q) => q.queryKey[0] !== "auth" });
}
