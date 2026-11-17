import { cn } from "cn";
import type { Torrent } from "@/api/types";
import { describeState, type StateInfo } from "@/lib/filters";
import { formatPercent } from "@/lib/format";

const toneText: Record<StateInfo["tone"], string> = {
  progress: "text-state-progress",
  success: "text-state-success",
  muted: "text-muted-foreground",
  warning: "text-state-warning",
  danger: "text-destructive",
  info: "text-state-info",
};

const toneBg: Record<StateInfo["tone"], string> = {
  progress: "bg-state-progress",
  success: "bg-state-success",
  muted: "bg-state-muted",
  warning: "bg-state-warning",
  danger: "bg-destructive",
  info: "bg-state-info",
};

type StateFields = Pick<Torrent, "state" | "complete" | "forced" | "error">;

/** The state as a coloured dot and label. */
export function StateBadge({ torrent, className }: { torrent: StateFields; className?: string }) {
  const info = describeState(torrent);
  return (
    <span
      className={cn("inline-flex min-w-0 items-center gap-1.5", toneText[info.tone], className)}
      title={torrent.error || undefined}
    >
      <span className={cn("size-1.5 shrink-0", toneBg[info.tone])} aria-hidden="true" />
      <span className="truncate">{info.label}</span>
    </span>
  );
}

/**
 * A thin progress bar with the percentage. Plain elements rather than a
 * Base UI Progress: a long list renders many of these on every update.
 */
export function ProgressBar({
  torrent,
  className,
  showLabel = true,
}: {
  torrent: Pick<Torrent, "progress" | "state" | "complete" | "forced" | "error">;
  className?: string;
  showLabel?: boolean;
}) {
  const info = describeState(torrent);
  const pct = Math.max(0, Math.min(1, torrent.progress)) * 100;
  return (
    <span className={cn("flex w-full min-w-0 items-center gap-2", className)}>
      <span
        className="relative h-1.5 flex-1 overflow-hidden bg-muted"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.floor(pct)}
        aria-label="Progress"
      >
        <span
          className={cn("absolute inset-y-0 left-0", toneBg[info.tone])}
          style={{ width: `${pct}%` }}
        />
      </span>
      {showLabel && (
        <span className="w-11 shrink-0 text-right tabular text-xs text-muted-foreground">
          {formatPercent(torrent.progress)}
        </span>
      )}
    </span>
  );
}
