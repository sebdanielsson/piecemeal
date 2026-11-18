// Turns a torrent's flat file list ("/"-separated paths) into a folder tree
// with sizes, progress and priorities rolled up, and flattens it again for a
// virtualized list.

import type { FilePriority, TorrentFile } from "@/api/types";

export interface FileLeaf {
  kind: "file";
  name: string;
  path: string;
  file: TorrentFile;
  size: number;
  done: number;
  priority: FilePriority;
  indices: number[];
}

export interface FolderNode {
  kind: "folder";
  name: string;
  path: string;
  children: TreeNode[];
  size: number;
  /** Bytes downloaded, from each file's size and progress. */
  done: number;
  /** The shared priority of every file below, or "mixed". */
  priority: FilePriority | "mixed";
  /** File indices below this folder, for setting their priority at once. */
  indices: number[];
}

export type TreeNode = FileLeaf | FolderNode;

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

/** Builds the tree under a root folder with an empty name and path. */
export function buildFileTree(files: readonly TorrentFile[]): FolderNode {
  const root = folder("", "");
  const folders = new Map<string, FolderNode>([["", root]]);
  for (const file of files) {
    const parts = file.path.split("/").filter(Boolean);
    const name = parts.pop() ?? file.path;
    let parent = root;
    let path = "";
    for (const part of parts) {
      path = path ? `${path}/${part}` : part;
      let next = folders.get(path);
      if (!next) {
        next = folder(part, path);
        folders.set(path, next);
        parent.children.push(next);
      }
      parent = next;
    }
    parent.children.push({
      kind: "file",
      name,
      path: path ? `${path}/${name}` : name,
      file,
      size: file.size,
      done: file.size * file.progress,
      priority: file.priority,
      indices: [file.index],
    });
  }
  rollUp(root);
  return root;
}

function folder(name: string, path: string): FolderNode {
  return { kind: "folder", name, path, children: [], size: 0, done: 0, priority: 1, indices: [] };
}

function rollUp(node: FolderNode): void {
  node.children.sort((a, b) =>
    a.kind === b.kind ? collator.compare(a.name, b.name) : a.kind === "folder" ? -1 : 1,
  );
  let priority: FilePriority | "mixed" | undefined;
  for (const child of node.children) {
    if (child.kind === "folder") {
      rollUp(child);
    }
    node.size += child.size;
    node.done += child.done;
    node.indices.push(...child.indices);
    if (priority === undefined) {
      priority = child.priority;
    } else if (priority !== child.priority) {
      priority = "mixed";
    }
  }
  node.priority = priority ?? 1;
}

export function nodeProgress(node: TreeNode): number {
  return node.size > 0 ? Math.min(1, node.done / node.size) : 1;
}

export interface FlatRow {
  node: TreeNode;
  depth: number;
}

/**
 * Lists the nodes to show, depth first, descending only into expanded
 * folders. The root itself is not listed. A single top-level folder (the
 * usual multi-file layout) is listed like any other.
 */
export function flattenTree(root: FolderNode, expanded: ReadonlySet<string>): FlatRow[] {
  const rows: FlatRow[] = [];
  const walk = (node: FolderNode, depth: number) => {
    for (const child of node.children) {
      rows.push({ node: child, depth });
      if (child.kind === "folder" && expanded.has(child.path)) {
        walk(child, depth + 1);
      }
    }
  };
  walk(root, 0);
  return rows;
}

/** Every folder path, for "expand all". */
export function folderPaths(root: FolderNode): string[] {
  const out: string[] = [];
  const walk = (node: FolderNode) => {
    for (const child of node.children) {
      if (child.kind === "folder") {
        out.push(child.path);
        walk(child);
      }
    }
  };
  walk(root);
  return out;
}

export const priorityLabels: Record<FilePriority, string> = {
  0: "Skip",
  1: "Normal",
  6: "High",
  7: "Highest",
};

export const priorities: readonly FilePriority[] = [7, 6, 1, 0];
