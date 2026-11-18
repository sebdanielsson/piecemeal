import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { Category, SettingsResponse, TransferStatus } from "@/api/types";
import { FakeEventSource } from "@/test/fake-event-source";
import { torrent, transfer } from "@/test/fixtures";
import { keys } from "../queries";
import type { TorrentMap, TransferSample } from "./reducer";
import { applyEvent, connectLiveStream, streamStatus } from "./stream";

let qc: QueryClient;
beforeEach(() => {
  qc = new QueryClient();
  FakeEventSource.reset();
});
afterEach(() => {
  vi.useRealTimers();
});

const a = torrent({ hash: "a".repeat(40) });
const b = torrent({ hash: "b".repeat(40) });

test("a snapshot replaces the torrents, transfer, categories and tags", () => {
  qc.setQueryData<TorrentMap>(keys.torrents, new Map([[b.hash, b]]));
  applyEvent(
    qc,
    "snapshot",
    {
      torrents: [a],
      transfer: transfer({ downloadRate: 7 }),
      categories: [{ name: "tv", savePath: "" }],
      tags: null,
    },
    1000,
  );
  expect([...(qc.getQueryData<TorrentMap>(keys.torrents)?.keys() ?? [])]).toEqual([a.hash]);
  expect(qc.getQueryData<TransferStatus>(keys.transfer)?.downloadRate).toBe(7);
  expect(qc.getQueryData<TransferSample[]>(keys.transferHistory)).toEqual([
    { at: 1000, down: 7, up: 0 },
  ]);
  expect(qc.getQueryData<Category[]>(keys.categories)).toEqual([{ name: "tv", savePath: "" }]);
  expect(qc.getQueryData<string[]>(keys.tags)).toEqual([]);
});

test("torrents, categories and tags events update the cache", () => {
  applyEvent(qc, "snapshot", { torrents: [a], transfer: transfer(), categories: [], tags: [] }, 0);
  applyEvent(qc, "torrents", { updated: [b], removed: [a.hash] }, 0);
  expect([...(qc.getQueryData<TorrentMap>(keys.torrents)?.keys() ?? [])]).toEqual([b.hash]);
  applyEvent(qc, "tags", { items: ["x"] }, 0);
  expect(qc.getQueryData(keys.tags)).toEqual(["x"]);
  applyEvent(qc, "categories", { items: null }, 0);
  expect(qc.getQueryData(keys.categories)).toEqual([]);
});

test("a settings event replaces the settings and which of them are locked", () => {
  const event = { settings: { addStopped: true }, locked: { dht: "PIECEMEAL_DHT" } };
  applyEvent(qc, "settings", event, 0);
  expect(qc.getQueryData<SettingsResponse>(keys.settings)).toEqual(event);
  applyEvent(qc, "settings", { settings: { addStopped: false }, locked: {} }, 0);
  expect(qc.getQueryData<SettingsResponse>(keys.settings)).toEqual({
    settings: { addStopped: false },
    locked: {},
  });
});

test("the stream goes live on its snapshot and applies later events", () => {
  const stop = connectLiveStream(qc, {
    onUnauthorized: vi.fn<() => void>(),
    EventSource: FakeEventSource as unknown as typeof EventSource,
  });
  const es = FakeEventSource.latest();
  expect(es.url).toBe("/api/piecemeal/v1/events");
  expect(streamStatus.get()).toBe("connecting");
  es.emit("snapshot", { torrents: [a], transfer: transfer(), categories: [], tags: [] });
  expect(streamStatus.get()).toBe("live");
  es.emit("torrents", { updated: [{ ...a, progress: 1 }], removed: [] });
  expect(qc.getQueryData<TorrentMap>(keys.torrents)?.get(a.hash)?.progress).toBe(1);
  // Garbage is ignored rather than crashing the stream.
  es.dispatchEvent(new MessageEvent("torrents", { data: "{not json" }));
  stop();
  expect(streamStatus.get()).toBe("stopped");
  expect(es.readyState).toBe(2);
});

test("while the browser reconnects by itself, the status says so", () => {
  const stop = connectLiveStream(qc, {
    onUnauthorized: vi.fn<() => void>(),
    EventSource: FakeEventSource as unknown as typeof EventSource,
  });
  FakeEventSource.latest().fail(0);
  expect(streamStatus.get()).toBe("reconnecting");
  expect(FakeEventSource.instances).toHaveLength(1);
  stop();
});

test("a failed connection retries with backoff while the session is valid", async () => {
  vi.useFakeTimers();
  const checkSession = vi.fn<() => Promise<boolean>>(async () => true);
  const stop = connectLiveStream(qc, {
    onUnauthorized: vi.fn<() => void>(),
    checkSession,
    EventSource: FakeEventSource as unknown as typeof EventSource,
  });
  FakeEventSource.latest().fail();
  await vi.advanceTimersByTimeAsync(999);
  expect(checkSession).toHaveBeenCalledOnce();
  expect(FakeEventSource.instances).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(1);
  expect(FakeEventSource.instances).toHaveLength(2);
  // The second failure waits twice as long.
  FakeEventSource.latest().fail();
  await vi.advanceTimersByTimeAsync(1999);
  expect(FakeEventSource.instances).toHaveLength(2);
  await vi.advanceTimersByTimeAsync(1);
  expect(FakeEventSource.instances).toHaveLength(3);
  stop();
});

test("a failed connection with an ended session signs out", async () => {
  const onUnauthorized = vi.fn<() => void>();
  connectLiveStream(qc, {
    onUnauthorized,
    checkSession: async () => false,
    EventSource: FakeEventSource as unknown as typeof EventSource,
  });
  FakeEventSource.latest().fail();
  await vi.waitFor(() => expect(onUnauthorized).toHaveBeenCalledOnce());
  expect(streamStatus.get()).toBe("stopped");
});
