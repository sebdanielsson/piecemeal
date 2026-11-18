import { describe, expect, it } from "vitest";
import { safeRedirect } from "./login-page";

describe("safeRedirect", () => {
  it("keeps same-site paths", () => {
    expect(safeRedirect("/torrents?x=1")).toBe("/torrents?x=1");
    expect(safeRedirect("/login")).toBe("/");
    expect(safeRedirect(undefined)).toBe("/");
  });

  it("refuses anything a browser could turn into another site", () => {
    for (const t of [
      "//evil.example",
      "/\\evil.example",
      "/\t/evil.example",
      "/\n/evil.example",
      "/ /evil",
      "https://evil.example",
    ]) {
      expect(safeRedirect(t)).toBe("/");
    }
  });
});
