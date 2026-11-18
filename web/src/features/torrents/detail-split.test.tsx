import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { DetailSplit } from "./detail-split";

test("the resize handle reports the height actually shown, within real bounds", () => {
  // Saved from a taller window than the 800px the test pretends to have.
  localStorage.setItem("piecemeal.detail.height", "5000");
  render(
    <DetailSplit open detail={<p>details</p>}>
      <p>list</p>
    </DetailSplit>,
  );
  const handle = screen.getByRole("separator", { name: "Resize details" });
  expect(handle.getAttribute("aria-valuemin")).toBe("160");
  expect(handle.getAttribute("aria-valuemax")).toBe("680");
  expect(handle.getAttribute("aria-valuenow")).toBe("680");
  expect(screen.getByRole("region", { name: "Torrent details" }).style.height).toBe("680px");
});
