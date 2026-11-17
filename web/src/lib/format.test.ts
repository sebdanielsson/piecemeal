import { describe, expect, test } from "vitest";
import {
  formatBytes,
  formatDuration,
  formatEta,
  formatLimit,
  formatPercent,
  formatRate,
  formatRatio,
  formatRelative,
  formatShareLimit,
  plural,
  trackerHost,
} from "./format";

describe("formatBytes", () => {
  test.each([
    [0, "0 B"],
    [512, "512 B"],
    [1023, "1023 B"],
    [1024, "1.00 KiB"],
    [1536, "1.50 KiB"],
    [10 * 1024, "10.0 KiB"],
    [500 * 1024, "500 KiB"],
    [3 * 1024 ** 2, "3.00 MiB"],
    [1.5 * 1024 ** 3, "1.50 GiB"],
    [2 * 1024 ** 4, "2.00 TiB"],
  ])("%d bytes is %s", (bytes, text) => {
    expect(formatBytes(bytes)).toBe(text);
  });

  test("rounding carries into the next unit", () => {
    expect(formatBytes(1024 * 1024 - 1)).toBe("1.00 MiB");
  });

  test("nonsense is a dash", () => {
    expect(formatBytes(-1)).toBe("–");
    expect(formatBytes(Number.NaN)).toBe("–");
  });
});

test("formatRate and formatLimit", () => {
  expect(formatRate(2048)).toBe("2.00 KiB/s");
  expect(formatLimit(0)).toBe("Unlimited");
  expect(formatLimit(1024 * 1024)).toBe("1.00 MiB/s");
});

describe("durations", () => {
  test.each([
    [0, "0s"],
    [59, "59s"],
    [61, "1m 1s"],
    [3600, "1h 0m"],
    [3 * 3600 + 25 * 60, "3h 25m"],
    [2 * 86400 + 5 * 3600, "2d 5h"],
    [400 * 86400, "1y 35d"],
  ])("%d seconds is %s", (s, text) => {
    expect(formatDuration(s)).toBe(text);
  });

  test("an unknown or absurd ETA is infinity, a finished one is empty", () => {
    expect(formatEta(-1)).toBe("∞");
    expect(formatEta(365 * 86400)).toBe("∞");
    expect(formatEta(0)).toBe("");
    expect(formatEta(90)).toBe("1m 30s");
  });
});

test("formatPercent never claims 100% early", () => {
  expect(formatPercent(0)).toBe("0%");
  expect(formatPercent(0.5)).toBe("50.0%");
  expect(formatPercent(0.9999)).toBe("99.9%");
  expect(formatPercent(1)).toBe("100%");
});

test("formatRatio", () => {
  expect(formatRatio(1.234)).toBe("1.23");
  expect(formatRatio(-1)).toBe("∞");
});

test("formatShareLimit", () => {
  expect(formatShareLimit(-2, "ratio")).toBe("Global");
  expect(formatShareLimit(-1, "minutes")).toBe("Unlimited");
  expect(formatShareLimit(2, "ratio")).toBe("2.00");
  expect(formatShareLimit(90, "minutes")).toBe("1h 30m");
});

test("trackerHost", () => {
  expect(trackerHost("")).toBe("");
  expect(trackerHost("udp://tracker.example.org:1337/announce")).toBe("tracker.example.org");
  expect(trackerHost("https://t.example.com/a?passkey=x")).toBe("t.example.com");
  expect(trackerHost("not a url")).toBe("not a url");
});

test("formatRelative", () => {
  const now = Date.parse("2026-01-01T12:00:00Z");
  expect(formatRelative(undefined, now)).toBe("never");
  expect(formatRelative(new Date(now - 10_000), now)).toBe("just now");
  expect(formatRelative(new Date(now - 5 * 60_000), now)).toMatch(/5 minutes ago/);
  expect(formatRelative(new Date(now - 3 * 86_400_000), now)).toMatch(/3 days ago/);
});

test("plural", () => {
  expect(plural(1, "torrent")).toBe("1 torrent");
  expect(plural(2, "torrent")).toBe("2 torrents");
  expect(plural(2, "entry", "entries")).toBe("2 entries");
});
