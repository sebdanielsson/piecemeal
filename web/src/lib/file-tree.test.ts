import { expect, test } from "vitest";
import type { TorrentFile } from "@/api/types";
import { buildFileTree, flattenTree, folderPaths, nodeProgress } from "./file-tree";

const file = (
  index: number,
  path: string,
  size: number,
  progress = 0,
  priority: TorrentFile["priority"] = 1,
): TorrentFile => ({
  index,
  path,
  size,
  progress,
  priority,
  pieceStart: 0,
  pieceEnd: 0,
});

const files = [
  file(0, "Album/CD 10/01.flac", 100, 1),
  file(1, "Album/CD 2/01.flac", 100, 0.5),
  file(2, "Album/cover.jpg", 50, 0, 0),
  file(3, "Album/CD 2/02.flac", 100, 0, 1),
];

test("builds folders from paths and rolls up size, progress and priority", () => {
  const root = buildFileTree(files);
  expect(root.size).toBe(350);
  const album = root.children[0];
  expect(album?.kind).toBe("folder");
  if (album?.kind !== "folder") {
    return;
  }
  expect(album.name).toBe("Album");
  expect(album.size).toBe(350);
  expect(album.done).toBe(150);
  expect(nodeProgress(album)).toBeCloseTo(150 / 350);
  // Folders first, then files, in natural order.
  expect(album.children.map((c) => c.name)).toEqual(["CD 2", "CD 10", "cover.jpg"]);
  expect(album.priority).toBe("mixed");
  expect(album.indices.toSorted()).toEqual([0, 1, 2, 3]);
  const cd2 = album.children[0];
  expect(cd2?.kind === "folder" && cd2.priority).toBe(1);
  expect(cd2?.indices).toEqual([1, 3]);
});

test("a single-file torrent is one file at the top", () => {
  const root = buildFileTree([file(0, "movie.mkv", 10)]);
  expect(root.children).toHaveLength(1);
  expect(root.children[0]?.kind).toBe("file");
  expect(root.children[0]?.path).toBe("movie.mkv");
});

test("flattens only expanded folders, with depth", () => {
  const root = buildFileTree(files);
  expect(flattenTree(root, new Set()).map((r) => r.node.path)).toEqual(["Album"]);
  const rows = flattenTree(root, new Set(["Album", "Album/CD 2"]));
  expect(rows.map((r) => [r.node.path, r.depth])).toEqual([
    ["Album", 0],
    ["Album/CD 2", 1],
    ["Album/CD 2/01.flac", 2],
    ["Album/CD 2/02.flac", 2],
    ["Album/CD 10", 1],
    ["Album/cover.jpg", 1],
  ]);
});

test("folderPaths lists every folder for expand all", () => {
  expect(folderPaths(buildFileTree(files))).toEqual(["Album", "Album/CD 2", "Album/CD 10"]);
});

test("an empty folder counts as done", () => {
  expect(nodeProgress(buildFileTree([]))).toBe(1);
});
