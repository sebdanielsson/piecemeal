import type { Tracker } from "@/api/types";

/** Trackers as text: one URL per line, a blank line between tiers. */
export function formatTiers(trackers: readonly Pick<Tracker, "tier" | "url">[]): string {
  const tiers: string[][] = [];
  for (const t of trackers.toSorted((a, b) => a.tier - b.tier)) {
    (tiers[t.tier] ??= []).push(t.url);
  }
  return tiers
    .filter((tier) => tier.length > 0)
    .map((tier) => tier.join("\n"))
    .join("\n\n");
}

/** Parses formatTiers' text back into tiers, dropping empty ones. */
export function parseTiers(text: string): string[][] {
  return text
    .split(/\n\s*\n/)
    .map((block) =>
      block
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean),
    )
    .filter((tier) => tier.length > 0);
}
