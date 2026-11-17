import { expect, test } from "vitest";
import { torrent } from "@/test/fixtures";
import { sortTorrents } from "./sort";

const t1 = torrent({ hash: "1".repeat(40), name: "Episode 10", size: 30, eta: 50, addedAt: 3 });
const t2 = torrent({ hash: "2".repeat(40), name: "episode 2", size: 10, eta: -1, addedAt: 1 });
const t3 = torrent({ hash: "3".repeat(40), name: "Episode 1", size: 30, eta: 10, addedAt: 2 });
const list = [t1, t2, t3];
const names = (l: { name: string }[]) => l.map((t) => t.name);

test("names sort naturally and ignore case", () => {
  expect(names(sortTorrents(list, { id: "name", desc: false }))).toEqual([
    "Episode 1",
    "episode 2",
    "Episode 10",
  ]);
});

test("descending, with ties broken by infohash in both directions", () => {
  expect(names(sortTorrents(list, { id: "size", desc: true }))).toEqual([
    "Episode 10",
    "Episode 1",
    "episode 2",
  ]);
  expect(names(sortTorrents(list, { id: "size", desc: false }))).toEqual([
    "episode 2",
    "Episode 10",
    "Episode 1",
  ]);
});

test("unknown ETAs sort after known ones", () => {
  expect(names(sortTorrents(list, { id: "eta", desc: false }))).toEqual([
    "Episode 1",
    "Episode 10",
    "episode 2",
  ]);
});

test("returns a copy, and keeps the order for an unknown column", () => {
  const out = sortTorrents(list, { id: "nope", desc: false });
  expect(out).not.toBe(list);
  expect(out).toEqual(list);
  expect(sortTorrents(list, undefined)).toEqual(list);
});
