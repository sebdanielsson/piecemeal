import { expect, test } from "vitest";
import type { LogEntry, Settings } from "@/api/types";
import { appendLog } from "./admin/log-page";
import { describeUserAgent } from "./admin/sessions-page";
import { cellSize, countPieces } from "./detail/pieces-tab";
import { settingsDiff, splitSettingsPatch } from "./settings/settings-form";
import { niceCeiling } from "./transfer/speed-chart";
import { shouldStart } from "./torrents/torrent-actions";

test("countPieces reads the API's piece string", () => {
  expect(countPieces("0012223")).toEqual({ missing: 2, downloading: 1, complete: 3, checking: 1 });
  expect(countPieces("")).toEqual({ missing: 0, downloading: 0, complete: 0, checking: 0 });
});

test("cellSize fits the piece map into its box", () => {
  expect(cellSize(0, 500, 300)).toBe(0);
  expect(cellSize(100, 500, 300)).toBe(12);
  // 100,000 pieces in a 1000×360 box only fit at one pixel each.
  expect(cellSize(100_000, 1000, 360)).toBe(1);
  const size = cellSize(5000, 1000, 360);
  expect(Math.ceil(5000 / Math.floor(1000 / size)) * size).toBeLessThanOrEqual(360);
});

test("niceCeiling picks a round top for the speed graph", () => {
  expect(niceCeiling(0)).toBe(100 * 1024);
  expect(niceCeiling(700)).toBe(1000);
  expect(niceCeiling(3.5 * 1024 * 1024)).toBe(5 * 1024 * 1024);
  expect(niceCeiling(1024)).toBe(1024);
});

const e = (id: number): LogEntry => ({
  id,
  time: "",
  level: "INFO",
  message: String(id),
  attrs: "",
});

test("appendLog drops repeats and keeps the newest entries", () => {
  const first = appendLog([], [e(1), e(2)]);
  expect(appendLog(first, [e(2)])).toBe(first);
  expect(appendLog(first, [e(2), e(3), e(4)], 3).map((x) => x.id)).toEqual([2, 3, 4]);
});

test("describeUserAgent", () => {
  expect(
    describeUserAgent(
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36",
    ),
  ).toBe("Chrome on macOS");
  expect(
    describeUserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"),
  ).toBe("Firefox on Linux");
  expect(describeUserAgent("curl/8.7.1")).toBe("curl/8.7.1");
  expect(describeUserAgent("")).toBe("Unknown client");
});

test("settingsDiff lists only what changed", () => {
  const base = { downloadLimit: 0, utp: true, encryption: "prefer" } as Settings;
  expect(settingsDiff(base, { ...base })).toEqual({});
  expect(settingsDiff(base, { ...base, downloadLimit: 1024, utp: false })).toEqual({
    downloadLimit: 1024,
    utp: false,
  });
});

test("toggling starts torrents if any is stopped", () => {
  expect(shouldStart([{ stopped: false }, { stopped: true }])).toBe(true);
  expect(shouldStart([{ stopped: false }])).toBe(false);
});

test("speed limits go to PATCH /transfer, everything else to PATCH /settings", () => {
  expect(
    splitSettingsPatch({ downloadLimit: 1024, altSpeedEnabled: true, utp: false, ratioLimit: 2 }),
  ).toEqual({
    transfer: { downloadLimit: 1024, altSpeedEnabled: true },
    settings: { utp: false, ratioLimit: 2 },
  });
  expect(splitSettingsPatch({})).toEqual({ transfer: {}, settings: {} });
});
