import { expect, test } from "vitest";
import { clickSelect, emptySelection, moveCursor, selectAll, visibleSelection } from "./selection";

const order = ["a", "b", "c", "d", "e"];
const sel = (s: { selected: ReadonlySet<string> }) => [...s.selected].toSorted();

test("a plain click selects one row", () => {
  const s = clickSelect(clickSelect(emptySelection, "b", order), "d", order);
  expect(sel(s)).toEqual(["d"]);
  expect(s.anchor).toBe("d");
});

test("Ctrl/Cmd-click toggles rows", () => {
  let s = clickSelect(emptySelection, "b", order);
  s = clickSelect(s, "d", order, { toggle: true });
  expect(sel(s)).toEqual(["b", "d"]);
  s = clickSelect(s, "b", order, { toggle: true });
  expect(sel(s)).toEqual(["d"]);
});

test("Shift-click selects the range from the anchor, either way", () => {
  let s = clickSelect(emptySelection, "b", order);
  s = clickSelect(s, "d", order, { range: true });
  expect(sel(s)).toEqual(["b", "c", "d"]);
  s = clickSelect(s, "a", order, { range: true });
  expect(sel(s)).toEqual(["a", "b"]);
});

test("Ctrl+Shift-click adds the range to the selection", () => {
  let s = clickSelect(emptySelection, "e", order);
  s = clickSelect(s, "a", order, { toggle: true });
  s = clickSelect(s, "c", order, { toggle: true, range: true });
  expect(sel(s)).toEqual(["a", "b", "c", "e"]);
});

test("arrow keys move the cursor and Shift extends", () => {
  let s = moveCursor(emptySelection, order, 1);
  expect(s.cursor).toBe("a");
  s = moveCursor(s, order, 1);
  expect(sel(s)).toEqual(["b"]);
  s = moveCursor(s, order, 1, true);
  s = moveCursor(s, order, 1, true);
  expect(sel(s)).toEqual(["b", "c", "d"]);
  s = moveCursor(s, order, 10);
  expect(s.cursor).toBe("e");
  expect(moveCursor(emptySelection, order, -1).cursor).toBe("e");
  expect(moveCursor(emptySelection, [], 1)).toBe(emptySelection);
});

test("select all, and the visible part of a selection in list order", () => {
  const s = selectAll(emptySelection, order);
  expect(s.selected.size).toBe(5);
  expect(visibleSelection(s, ["e", "x", "a"])).toEqual(["e", "a"]);
});
