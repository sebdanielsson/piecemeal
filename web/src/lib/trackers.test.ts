import { describe, expect, it } from "vitest";
import { formatTiers, parseTiers } from "./trackers";

describe("tracker tiers", () => {
  it("round-trips", () => {
    const trackers = [
      { tier: 1, url: "udp://c" },
      { tier: 0, url: "udp://a" },
      { tier: 0, url: "udp://b" },
    ];
    const text = formatTiers(trackers);
    expect(text).toBe("udp://a\nudp://b\n\nudp://c");
    expect(parseTiers(text)).toEqual([["udp://a", "udp://b"], ["udp://c"]]);
  });

  it("ignores extra blank lines and spaces", () => {
    expect(parseTiers("\n  udp://a  \n\n\n \nudp://b\n")).toEqual([["udp://a"], ["udp://b"]]);
  });
});
