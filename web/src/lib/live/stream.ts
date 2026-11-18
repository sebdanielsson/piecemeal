// Keeps the TanStack Query cache current from the server-sent event stream
// at /events. The first event on every connection is a full snapshot, so a
// reconnect simply starts over from it.

import type { QueryClient } from "@tanstack/react-query";
import { apiPrefix } from "@/api/client";
import type {
  Category,
  SettingsResponse,
  SnapshotEvent,
  TorrentsEvent,
  TransferStatus,
} from "@/api/types";
import { keys } from "../queries";
import { createStore } from "../store";
import {
  appendTransferSample,
  applyTorrentsEvent,
  torrentMapFrom,
  type TorrentMap,
  type TransferSample,
} from "./reducer";

export type StreamStatus = "connecting" | "live" | "reconnecting" | "stopped";

/** The stream's connection state, for the status indicator. */
export const streamStatus = createStore<StreamStatus>("stopped");

export interface StreamOptions {
  /** Called when reconnecting fails because the session has ended. */
  onUnauthorized: () => void;
  /** Checks whether the session is still valid; defaults to GET /auth/me. */
  checkSession?: () => Promise<boolean>;
  /** For tests. */
  EventSource?: typeof EventSource;
  now?: () => number;
}

const maxBackoff = 30_000;

/** Applies one named event to the cache. Exported for tests. */
export function applyEvent(qc: QueryClient, type: string, data: unknown, now: number): void {
  switch (type) {
    case "snapshot": {
      const snap = data as SnapshotEvent;
      qc.setQueryData<TorrentMap>(keys.torrents, torrentMapFrom(snap.torrents));
      applyTransfer(qc, snap.transfer, now);
      qc.setQueryData<Category[]>(keys.categories, snap.categories ?? []);
      qc.setQueryData<string[]>(keys.tags, snap.tags ?? []);
      break;
    }
    case "torrents":
      qc.setQueryData<TorrentMap>(keys.torrents, (prev) =>
        applyTorrentsEvent(prev, data as TorrentsEvent),
      );
      break;
    case "transfer":
      applyTransfer(qc, data as TransferStatus, now);
      break;
    case "categories":
      qc.setQueryData<Category[]>(
        keys.categories,
        (data as { items: Category[] | null }).items ?? [],
      );
      break;
    case "tags":
      qc.setQueryData<string[]>(keys.tags, (data as { items: string[] | null }).items ?? []);
      break;
    case "settings":
      qc.setQueryData<SettingsResponse>(keys.settings, data as SettingsResponse);
      break;
  }
}

function applyTransfer(qc: QueryClient, transfer: TransferStatus, now: number) {
  qc.setQueryData<TransferStatus>(keys.transfer, transfer);
  qc.setQueryData<TransferSample[]>(keys.transferHistory, (prev) =>
    appendTransferSample(prev, transfer, now),
  );
}

const eventTypes = ["snapshot", "torrents", "transfer", "categories", "tags", "settings"];

/** Opens the stream and returns a function that closes it. */
export function connectLiveStream(qc: QueryClient, options: StreamOptions): () => void {
  const ES = options.EventSource ?? globalThis.EventSource;
  const now = options.now ?? Date.now;
  const checkSession =
    options.checkSession ??
    (async () => {
      const res = await fetch(`${apiPrefix}/auth/me`, { credentials: "same-origin" });
      return res.status !== 401;
    });
  let source: EventSource | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let attempt = 0;
  let stopped = false;

  const connect = () => {
    streamStatus.set(attempt === 0 ? "connecting" : "reconnecting");
    const es = new ES(`${apiPrefix}/events`, { withCredentials: true });
    source = es;
    for (const type of eventTypes) {
      es.addEventListener(type, (ev) => {
        let data: unknown;
        try {
          data = JSON.parse((ev as MessageEvent<string>).data);
        } catch {
          return;
        }
        if (type === "snapshot") {
          attempt = 0;
          streamStatus.set("live");
        }
        applyEvent(qc, type, data, now());
      });
    }
    es.addEventListener("error", () => {
      if (stopped) {
        return;
      }
      if (es.readyState !== ES.CLOSED) {
        // The browser reconnects by itself; the next snapshot resets state.
        streamStatus.set("reconnecting");
        return;
      }
      // A failed connection, such as a 401 or a proxy error: the browser
      // gives up, so find out why and retry with backoff.
      es.close();
      streamStatus.set("reconnecting");
      attempt++;
      void checkSession()
        .catch(() => true)
        .then((valid) => {
          if (stopped) {
            return;
          }
          if (!valid) {
            streamStatus.set("stopped");
            options.onUnauthorized();
            return;
          }
          const delay = Math.min(maxBackoff, 1000 * 2 ** Math.min(attempt - 1, 5));
          timer = setTimeout(connect, delay);
        });
    });
  };

  connect();
  return () => {
    stopped = true;
    clearTimeout(timer);
    source?.close();
    streamStatus.set("stopped");
  };
}
