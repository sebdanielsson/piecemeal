import { describe, expect, test } from "vitest";
import { torrent } from "@/test/fixtures";
import {
  countTorrents,
  describeState,
  filterTorrents,
  matchesState,
  stateFilters,
} from "./filters";

const downloading = torrent({
  hash: "1".repeat(40),
  name: "Ubuntu ISO",
  state: "downloading",
  downloadRate: 100,
});
const stalled = torrent({ hash: "2".repeat(40), name: "Debian", state: "stalledDownloading" });
const seeding = torrent({
  hash: "3".repeat(40),
  name: "Music",
  state: "seeding",
  complete: true,
  progress: 1,
  uploadRate: 5,
  category: "music",
  tags: ["flac", "favorite"],
  tracker: "udp://tracker.example.org:1337/announce",
});
const stopped = torrent({
  hash: "4".repeat(40),
  name: "Old",
  state: "stopped",
  stopped: true,
  complete: true,
  category: "linux",
});
const errored = torrent({
  hash: "5".repeat(40),
  name: "Broken",
  state: "error",
  error: "disk full",
  tags: ["favorite"],
});
const checking = torrent({
  hash: "6".repeat(40),
  name: "Checking",
  state: "checking",
  tracker: "https://t.example.com/announce",
});
const all = [downloading, stalled, seeding, stopped, errored, checking];

const names = (list: { name: string }[]) => list.map((t) => t.name);

describe("state filters match the server's", () => {
  test.each([
    ["all", ["Ubuntu ISO", "Debian", "Music", "Old", "Broken", "Checking"]],
    ["downloading", ["Ubuntu ISO", "Debian", "Checking"]],
    ["seeding", ["Music"]],
    ["completed", ["Music", "Old"]],
    ["stopped", ["Old"]],
    ["active", ["Ubuntu ISO", "Music"]],
    ["inactive", ["Debian", "Old", "Broken", "Checking"]],
    ["stalled", ["Debian"]],
    ["checking", ["Checking"]],
    ["errored", ["Broken"]],
  ] as const)("%s", (filter, expected) => {
    expect(names(all.filter((t) => matchesState(t, filter)))).toEqual(expected);
  });
});

test("checking includes torrents queued for checking, as the server's filter does", () => {
  const waiting = torrent({ hash: "7".repeat(40), name: "Waiting", state: "queuedForChecking" });
  expect(matchesState(waiting, "checking")).toBe(true);
  expect(matchesState(waiting, "stalled")).toBe(false);
});

describe("filterTorrents", () => {
  test("by category, including none", () => {
    expect(names(filterTorrents(all, { state: "all", category: "music" }))).toEqual(["Music"]);
    expect(filterTorrents(all, { state: "all", category: "" })).toHaveLength(4);
  });

  test("by tag, including untagged", () => {
    expect(names(filterTorrents(all, { state: "all", tag: "favorite" }))).toEqual([
      "Music",
      "Broken",
    ]);
    expect(filterTorrents(all, { state: "all", tag: "" })).toHaveLength(4);
  });

  test("by tracker host, including trackerless", () => {
    expect(names(filterTorrents(all, { state: "all", tracker: "tracker.example.org" }))).toEqual([
      "Music",
    ]);
    expect(filterTorrents(all, { state: "all", tracker: "" })).toHaveLength(4);
  });

  test("by name, ignoring case and spaces around, or by infohash prefix", () => {
    expect(names(filterTorrents(all, { state: "all", search: "  ubuntu " }))).toEqual([
      "Ubuntu ISO",
    ]);
    expect(names(filterTorrents(all, { state: "all", search: "3333" }))).toEqual(["Music"]);
  });

  test("combines filters", () => {
    expect(names(filterTorrents(all, { state: "completed", tag: "favorite" }))).toEqual(["Music"]);
    expect(filterTorrents(all, { state: "stopped", category: "music" })).toEqual([]);
  });
});

test("countTorrents counts every sidebar entry in one pass", () => {
  const c = countTorrents(all);
  expect(c.total).toBe(6);
  expect(c.states.all).toBe(6);
  expect(c.states.downloading).toBe(3);
  expect(c.states.errored).toBe(1);
  expect(Object.keys(c.states)).toEqual([...stateFilters]);
  expect(c.categories.get("")).toBe(4);
  expect(c.categories.get("music")).toBe(1);
  expect(c.tags.get("favorite")).toBe(2);
  expect(c.tags.get("")).toBe(4);
  expect(c.trackers.get("t.example.com")).toBe(1);
  expect(c.trackers.get("")).toBe(4);
});

test("describeState", () => {
  expect(describeState(stopped)).toEqual({ label: "Completed", tone: "muted" });
  expect(describeState({ ...stopped, complete: false })).toEqual({
    label: "Stopped",
    tone: "muted",
  });
  expect(describeState({ ...downloading, forced: true }).label).toBe("Downloading (forced)");
  expect(describeState(errored).tone).toBe("danger");
  expect(describeState({ ...checking, state: "queuedForChecking" }).label).toBe(
    "Queued for checking",
  );
});
