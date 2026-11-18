import { expect, test } from "vitest";
import { limitFromParts, limitParts } from "./limit-input";

test("limits are shown in the friendliest unit", () => {
  expect(limitParts(0)).toEqual({ amount: "", unit: "KiB" });
  expect(limitParts(512 * 1024)).toEqual({ amount: "512", unit: "KiB" });
  expect(limitParts(5 * 1024 * 1024)).toEqual({ amount: "5", unit: "MiB" });
  expect(limitParts(1.5 * 1024 * 1024)).toEqual({ amount: "1.5", unit: "MiB" });
});

test("edits become bytes per second; empty, zero and junk are unlimited", () => {
  expect(limitFromParts("500", "KiB")).toBe(512_000);
  expect(limitFromParts("1,5", "MiB")).toBe(1_572_864);
  expect(limitFromParts("", "KiB")).toBe(0);
  expect(limitFromParts("0", "MiB")).toBe(0);
  expect(limitFromParts("fast", "KiB")).toBe(0);
});
