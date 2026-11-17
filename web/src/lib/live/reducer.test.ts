import { describe, expect, test } from "vitest";
import { torrent } from "@/test/fixtures";
import {
  appendTransferSample,
  applyTorrentsEvent,
  removeTorrents,
  torrentMapFrom,
  upsertTorrent,
} from "./reducer";

const a = torrent({ hash: "a".repeat(40), name: "A" });
const b = torrent({ hash: "b".repeat(40), name: "B" });
const c = torrent({ hash: "c".repeat(40), name: "C" });

describe("applyTorrentsEvent", () => {
  test("updates changed torrents and keeps the identity of the rest", () => {
    const prev = torrentMapFrom([a, b]);
    const a2 = { ...a, progress: 0.9 };
    const next = applyTorrentsEvent(prev, { updated: [a2], removed: [] });
    expect(next).not.toBe(prev);
    expect(next.get(a.hash)).toBe(a2);
    expect(next.get(b.hash)).toBe(b);
    // The previous map is untouched, so React sees a new value.
    expect(prev.get(a.hash)).toBe(a);
  });

  test("adds new torrents and removes deleted ones", () => {
    const next = applyTorrentsEvent(torrentMapFrom([a, b]), { updated: [c], removed: [a.hash] });
    expect([...next.keys()]).toEqual([b.hash, c.hash]);
  });

  test("returns the same map when nothing changes", () => {
    const prev = torrentMapFrom([a]);
    expect(applyTorrentsEvent(prev, { updated: [], removed: ["f".repeat(40)] })).toBe(prev);
    expect(applyTorrentsEvent(prev, { updated: null, removed: null })).toBe(prev);
  });

  test("starts from nothing before the snapshot", () => {
    const next = applyTorrentsEvent(undefined, { updated: [a], removed: null });
    expect(next.get(a.hash)).toBe(a);
  });

  test("a torrent removed and re-added in one event is kept", () => {
    // The server drops a hash from "removed" when it reappears in "updated",
    // but the client shouldn't care about the order either way.
    const next = applyTorrentsEvent(torrentMapFrom([a]), { updated: [b], removed: [a.hash] });
    expect(next.has(a.hash)).toBe(false);
    expect(next.has(b.hash)).toBe(true);
  });
});

test("torrentMapFrom handles a null list", () => {
  expect(torrentMapFrom(null).size).toBe(0);
});

test("upsertTorrent and removeTorrents", () => {
  const m = upsertTorrent(torrentMapFrom([a]), b);
  expect(m.size).toBe(2);
  const r = removeTorrents(m, [a.hash]);
  expect([...r.keys()]).toEqual([b.hash]);
  expect(removeTorrents(r, ["x"])).toBe(r);
});

const t = (down: number, up = 0) => ({ downloadRate: down, uploadRate: up });

describe("appendTransferSample", () => {
  test("appends one sample per event", () => {
    const h = appendTransferSample(appendTransferSample(undefined, t(1), 1000), t(2), 2000);
    expect(h).toEqual([
      { at: 1000, down: 1, up: 0 },
      { at: 2000, down: 2, up: 0 },
    ]);
  });

  test("a sample right after the last one replaces it", () => {
    const h = appendTransferSample(appendTransferSample(undefined, t(1), 1000), t(5), 1200);
    expect(h).toEqual([{ at: 1200, down: 5, up: 0 }]);
  });

  test("drops the oldest beyond the limit", () => {
    let h = appendTransferSample(undefined, t(0), 0, 3);
    for (let i = 1; i < 6; i++) {
      h = appendTransferSample(h, t(i), i * 1000, 3);
    }
    expect(h.map((s) => s.down)).toEqual([3, 4, 5]);
  });
});
