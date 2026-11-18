import { createStore } from "@/lib/store";

/** What to prefill when the add dialog opens. */
export interface AddRequest {
  tab?: "magnet" | "file" | "url";
  magnets?: string;
  files?: File[];
}

/** The add dialog's state: null while closed. */
export const addDialog = createStore<AddRequest | null>(null);

export function openAddDialog(request: AddRequest = {}): void {
  addDialog.set(request);
}

/** Magnet links and bare infohashes in pasted text, one per line. */
export function extractMagnets(text: string): string[] {
  const out: string[] = [];
  for (const raw of text.split(/\s+/)) {
    const s = raw.trim();
    if (/^magnet:\?/i.test(s)) {
      out.push(s);
    } else if (/^[0-9a-f]{40}$/i.test(s) || /^[a-z2-7]{32}$/i.test(s)) {
      out.push(`magnet:?xt=urn:btih:${s}`);
    }
  }
  return out;
}

export function isTorrentFile(file: File): boolean {
  return file.name.toLowerCase().endsWith(".torrent") || file.type === "application/x-bittorrent";
}
