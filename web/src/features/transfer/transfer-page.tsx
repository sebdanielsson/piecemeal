import { useState, type ReactNode } from "react";
import { OptionSelect } from "@/components/form/option-select";
import { formatBytes, formatLimit, formatRate } from "@/lib/format";
import { useTransfer, useTransferHistory } from "@/lib/live/hooks";
import { altSpeedGroup, scheduleGroup, speedGroup } from "../settings/groups";
import { SettingsForm } from "../settings/settings-form";
import { Page, PageSection } from "../shell/page";
import { SpeedChart } from "./speed-chart";

const windows = [
  { value: "60000", label: "1 minute" },
  { value: "300000", label: "5 minutes" },
  { value: "900000", label: "15 minutes" },
];

export function TransferPage() {
  const transfer = useTransfer();
  const history = useTransferHistory();
  const [windowMs, setWindowMs] = useState("300000");
  return (
    <Page
      title="Transfer"
      description="Live rates over the last 15 minutes, recorded while the web UI is connected, and the global speed limits."
    >
      <PageSection
        title="Speed"
        actions={
          <OptionSelect
            aria-label="Time window"
            value={windowMs}
            onChange={setWindowMs}
            options={windows}
            className="w-32 text-xs"
          />
        }
      >
        <SpeedChart samples={history} windowMs={Number(windowMs)} />
      </PageSection>
      {transfer && (
        <PageSection title="Session">
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Stat
              label="Download rate"
              value={formatRate(transfer.downloadRate)}
              sub={`Limit: ${formatLimit(transfer.downloadLimit)}`}
            />
            <Stat
              label="Upload rate"
              value={formatRate(transfer.uploadRate)}
              sub={`Limit: ${formatLimit(transfer.uploadLimit)}`}
            />
            <Stat
              label="Downloaded"
              value={formatBytes(transfer.sessionDownloaded)}
              sub="since piecemeal started"
            />
            <Stat
              label="Uploaded"
              value={formatBytes(transfer.sessionUploaded)}
              sub="since piecemeal started"
            />
            <Stat
              label="Peers"
              value={transfer.peers.toLocaleString()}
              sub={`${transfer.torrents.toLocaleString()} torrents`}
            />
            <Stat label="DHT nodes" value={transfer.dhtNodes.toLocaleString()} />
            <Stat
              label="Connection"
              value={transfer.connection[0]?.toUpperCase() + transfer.connection.slice(1)}
              sub={`Port ${transfer.port}`}
            />
            <Stat
              label="Free space"
              value={transfer.freeSpace >= 0 ? formatBytes(transfer.freeSpace) : "Unknown"}
            />
          </dl>
        </PageSection>
      )}
      <SettingsForm groups={[speedGroup, altSpeedGroup, scheduleGroup]} />
    </Page>
  );
}

function Stat({ label, value, sub }: { label: string; value: ReactNode; sub?: string }) {
  return (
    <div className="flex flex-col gap-1 border p-3">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-lg font-medium tabular">{value}</dd>
      {sub && <dd className="text-xs text-muted-foreground">{sub}</dd>}
    </div>
  );
}
