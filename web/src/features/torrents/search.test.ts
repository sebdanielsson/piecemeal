import { expect, test } from "vitest";
import { listFilterFrom, parseListSearch } from "./search";

test("keeps known values and drops the rest", () => {
  expect(parseListSearch({ filter: "seeding", category: "tv", q: "x", junk: 1 })).toEqual({
    filter: "seeding",
    category: "tv",
    q: "x",
  });
  expect(parseListSearch({ filter: "bogus" })).toEqual({});
  // "all" is the default, so it isn't kept in the URL.
  expect(parseListSearch({ filter: "all" })).toEqual({});
});

test("an empty string selects torrents without a category, tag or tracker", () => {
  const search = parseListSearch({ category: "", tag: "", tracker: "" });
  expect(search).toEqual({ category: "", tag: "", tracker: "" });
  expect(listFilterFrom(search)).toEqual({
    state: "all",
    category: "",
    tag: "",
    tracker: "",
    search: undefined,
  });
});

test("numbers from the URL parser become strings", () => {
  expect(parseListSearch({ tag: 2024 })).toEqual({ tag: "2024" });
});
