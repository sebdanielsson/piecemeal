import { useQuery } from "@tanstack/react-query";
import { DownloadIcon, MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import type { ReactNode } from "react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { formatDateTime, formatRelative } from "@/lib/format";
import { apiPrefix } from "@/api/client";
import { buttonVariants } from "@/components/ui/button";
import { meQuery, systemQuery } from "@/lib/queries";
import { useTheme, type ThemePreference } from "@/lib/theme";
import { Page, PageSection } from "../shell/page";
import { settingsGroups } from "./groups";
import { SettingsForm } from "./settings-form";

export function SettingsPage() {
  return (
    <Page title="Settings" description="Changes apply at once, without restarting piecemeal.">
      <Appearance />
      <SettingsForm
        groups={settingsGroups}
        footer={
          <>
            <SystemInfo />
            <Backup />
          </>
        }
      />
    </Page>
  );
}

function Appearance() {
  const { preference, setPreference } = useTheme();
  return (
    <PageSection title="Appearance" description="Stored in this browser.">
      <ToggleGroup
        value={[preference]}
        onValueChange={(v: string[]) => v[0] && setPreference(v[0] as ThemePreference)}
        aria-label="Theme"
        variant="outline"
      >
        <ToggleGroupItem value="system" aria-label="System theme">
          <MonitorIcon />
          System
        </ToggleGroupItem>
        <ToggleGroupItem value="light" aria-label="Light theme">
          <SunIcon />
          Light
        </ToggleGroupItem>
        <ToggleGroupItem value="dark" aria-label="Dark theme">
          <MoonIcon />
          Dark
        </ToggleGroupItem>
      </ToggleGroup>
    </PageSection>
  );
}

function Backup() {
  const { data: me } = useQuery(meQuery);
  if (!me?.isAdmin) {
    return null;
  }
  return (
    <PageSection
      title="Backup"
      description="Settings, users, API keys, torrents, categories, tags and RSS, but not the downloaded data. Restore it with piecemeal restore FILE while piecemeal is stopped."
    >
      <div>
        <a
          href={`${apiPrefix}/admin/backup`}
          download
          className={buttonVariants({ size: "sm", variant: "outline" })}
        >
          <DownloadIcon data-icon="inline-start" />
          Download backup
        </a>
      </div>
    </PageSection>
  );
}

function SystemInfo() {
  const { data: system } = useQuery(systemQuery);
  if (!system) {
    return null;
  }
  const nat = system.natpmp;
  return (
    <PageSection title="System" description="Set at startup by piecemeal's configuration.">
      <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-[12rem_1fr]">
        <Row label="Version">{system.version}</Row>
        <Row label="Running since">
          {formatDateTime(system.startedAt)}{" "}
          <span className="text-muted-foreground">({formatRelative(system.startedAt)})</span>
        </Row>
        <Row label="Peer port">{system.port}</Row>
        <Row label="Listening on">
          <span className="font-mono text-xs">{(system.listenAddrs ?? []).join(", ") || "–"}</span>
        </Row>
        <Row label="Port forwarding">
          {nat ? (
            nat.error ? (
              <span className="text-destructive">
                NAT-PMP via {nat.gateway} failed: {nat.error}
              </span>
            ) : nat.port > 0 ? (
              <>
                NAT-PMP via {nat.gateway}: port {nat.port}, renewed {formatRelative(nat.renewedAt)}
              </>
            ) : (
              <>NAT-PMP via {nat.gateway}: waiting for a mapping</>
            )
          ) : (
            <span className="text-muted-foreground">Not configured</span>
          )}
        </Row>
        <Row label="Download directory">
          <span className="font-mono text-xs">{system.downloadDir}</span>
        </Row>
        <Row label="Allowed save paths">
          <span className="font-mono text-xs">{(system.allowedRoots ?? []).join(", ") || "–"}</span>
        </Row>
        {system.watchDir && (
          <Row label="Watch folder">
            <span className="font-mono text-xs">{system.watchDir}</span>
          </Row>
        )}
        <Row label="Data directory">
          <span className="font-mono text-xs">{system.dataDir}</span>
        </Row>
      </dl>
    </PageSection>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  );
}
