import { describe, expect, it } from "vitest";
import { htmlToText } from "./html";

describe("htmlToText", () => {
  it("drops markup and scripts", () => {
    expect(htmlToText("<p>Episode <b>one</b></p><script>alert(1)</script>")).toBe("Episode one");
    expect(htmlToText('<img src=x onerror="alert(1)">1.2 GB &amp; more')).toBe("1.2 GB & more");
    expect(htmlToText("  plain  ")).toBe("plain");
  });
});
