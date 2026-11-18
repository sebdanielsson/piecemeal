import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  HardDriveIcon,
  NetworkIcon,
  TurtleIcon,
  UsersIcon,
} from "lucide-react";
import { cn } from "cn";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatBytes, formatRate } from "@/lib/format";
import { useTransfer } from "@/lib/live/hooks";
import { streamStatus } from "@/lib/live/stream";
import { useUpdateTransfer } from "@/lib/mutations";
import { settingsQuery } from "@/lib/queries";
import { useStore } from "@/lib/store";

const connectionLabels = {
  connected: "Connected",
  firewalled: "Firewalled",
  disconnected: "Disconnected",
} as const;

/** The status bar under every page: rates, limits, alternative speeds and connectivity. */
export function TransferBar() {
  const transfer = useTransfer();
  const status = useStore(streamStatus);
  const { data: settings } = useQuery(settingsQuery);
  const update = useUpdateTransfer();
  const alt = transfer?.altSpeedEnabled ?? settings?.settings.altSpeedEnabled ?? false;
  const altLocked = settings?.locked.altSpeedEnabled;
  const canToggle = transfer !== undefined && !altLocked;

  return (
    <footer
      className="flex h-10 shrink-0 items-center gap-1 border-t bg-sidebar px-2 text-xs text-sidebar-foreground sm:gap-3 sm:px-3"
      aria-label="Transfer status"
    >
      <SidebarTrigger className="md:hidden" />
      <Link
        to="/transfer"
        className="flex items-center gap-3 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none"
        aria-label="Transfer details"
      >
        <span className="flex items-center gap-1 tabular" title="Download rate">
          <ArrowDownIcon className="size-3.5 text-download" aria-label="Download" />
          {transfer ? formatRate(transfer.downloadRate) : "–"}
          {transfer && transfer.downloadLimit > 0 && (
            <span className="hidden text-muted-foreground lg:inline">
              / {formatRate(transfer.downloadLimit)}
            </span>
          )}
        </span>
        <span className="flex items-center gap-1 tabular" title="Upload rate">
          <ArrowUpIcon className="size-3.5 text-upload" aria-label="Upload" />
          {transfer ? formatRate(transfer.uploadRate) : "–"}
          {transfer && transfer.uploadLimit > 0 && (
            <span className="hidden text-muted-foreground lg:inline">
              / {formatRate(transfer.uploadLimit)}
            </span>
          )}
        </span>
      </Link>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              aria-pressed={alt}
              disabled={!canToggle || update.isPending}
              onClick={() => update.mutate({ altSpeedEnabled: !alt })}
              className={cn(
                "flex size-7 items-center justify-center hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none disabled:cursor-default disabled:hover:bg-transparent",
                alt ? "text-state-warning" : "text-muted-foreground",
              )}
            />
          }
        >
          <TurtleIcon className="size-4" />
          <span className="sr-only">Alternative speed limits</span>
        </TooltipTrigger>
        <TooltipContent>
          Alternative speed limits {alt ? "on" : "off"}
          {altLocked ? ` (set by ${altLocked})` : ""}
        </TooltipContent>
      </Tooltip>
      <div className="flex-1" />
      {status !== "live" && status !== "stopped" && (
        <span className="flex items-center gap-1.5 text-state-warning" role="status">
          <span className="size-1.5 animate-pulse bg-current" />
          {status === "connecting" ? "Connecting…" : "Reconnecting…"}
        </span>
      )}
      {transfer && (
        <>
          <span className="hidden items-center gap-1 tabular md:flex" title="Connected peers">
            <UsersIcon className="size-3.5 text-muted-foreground" />
            {transfer.peers.toLocaleString()}
          </span>
          {transfer.freeSpace >= 0 && (
            <span
              className="hidden items-center gap-1 tabular sm:flex"
              title="Free space in the default save path"
            >
              <HardDriveIcon className="size-3.5 text-muted-foreground" />
              {formatBytes(transfer.freeSpace)}
              <span className="hidden text-muted-foreground lg:inline">free</span>
            </span>
          )}
          <Tooltip>
            <TooltipTrigger
              render={
                <span
                  tabIndex={0}
                  className="flex items-center gap-1.5 tabular focus-visible:outline-none"
                />
              }
            >
              <span
                className={cn(
                  "size-2",
                  transfer.connection === "connected"
                    ? "bg-state-success"
                    : transfer.connection === "firewalled"
                      ? "bg-state-warning"
                      : "bg-destructive",
                )}
              />
              <NetworkIcon className="hidden size-3.5 text-muted-foreground sm:block" />
              <span className="hidden sm:inline">{transfer.port}</span>
            </TooltipTrigger>
            <TooltipContent>
              {connectionLabels[transfer.connection]} on port {transfer.port}
              {transfer.dhtNodes > 0 ? `, ${transfer.dhtNodes.toLocaleString()} DHT nodes` : ""}
              <br />
              This session: ↓ {formatBytes(transfer.sessionDownloaded)}, ↑{" "}
              {formatBytes(transfer.sessionUploaded)}
            </TooltipContent>
          </Tooltip>
        </>
      )}
    </footer>
  );
}
