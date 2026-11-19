import { CopyIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import type { Torrent, UpdateTorrentRequest } from "@/api/types";
import { CategorySelect } from "@/components/form/category-select";
import { OptionSelect } from "@/components/form/option-select";
import {
  optionLabel,
  seedGoalActions,
  seedGoalModes,
  withDefault,
} from "@/components/form/seed-goal-options";
import { ShareLimitInput } from "@/components/form/share-limit-input";
import { TagPicker } from "@/components/form/tag-picker";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { copyText } from "@/lib/clipboard";
import {
  formatBytes,
  formatDuration,
  formatEta,
  formatRate,
  formatRatio,
  formatShareLimit,
  formatUnixTime,
} from "@/lib/format";
import { useUpdateTorrent } from "@/lib/mutations";

export function GeneralTab({ torrent: t }: { torrent: Torrent }) {
  const update = useUpdateTorrent(t.hash);
  const patch = (body: UpdateTorrentRequest) => update.mutate(body);

  return (
    <div className="grid gap-x-8 gap-y-6 p-4 lg:grid-cols-2 2xl:grid-cols-3">
      {t.error && (
        <p
          role="alert"
          className="border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive lg:col-span-2 2xl:col-span-3"
        >
          {t.error}
        </p>
      )}
      <Section title="Transfer">
        <Item label="Downloaded">
          {formatBytes(t.completed)} of {formatBytes(t.size)}
          {t.downloaded !== t.completed && (
            <span className="text-muted-foreground">
              {" "}
              ({formatBytes(t.downloaded)} transferred)
            </span>
          )}
        </Item>
        <Item label="Uploaded">{formatBytes(t.uploaded)}</Item>
        <Item label="Ratio">{formatRatio(t.ratio)}</Item>
        <Item label="Download rate">{formatRate(t.downloadRate)}</Item>
        <Item label="Upload rate">{formatRate(t.uploadRate)}</Item>
        <Item label="ETA">{t.complete ? "Done" : formatEta(t.eta) || "–"}</Item>
        <Item label="Seeds">{t.seeds} connected</Item>
        <Item label="Peers">
          {t.peers} connected, {t.swarmPeers} in swarm
        </Item>
        <Item label="Active for">{formatDuration(t.activeTime)}</Item>
        <Item label="Seeding for">{formatDuration(t.seedingTime)}</Item>
        <Item label="Last activity">{formatUnixTime(t.lastActivityAt)}</Item>
      </Section>

      <Section title="Information">
        <Item label="Size">
          {formatBytes(t.size)}
          {t.totalSize !== t.size && (
            <span className="text-muted-foreground"> of {formatBytes(t.totalSize)}</span>
          )}
        </Item>
        <Item label="Pieces">
          {t.hasMetadata
            ? `${t.piecesHave.toLocaleString()} of ${t.pieces.toLocaleString()} × ${formatBytes(t.pieceSize)}`
            : "Waiting for metadata"}
        </Item>
        <Item label="Infohash">
          <Copyable text={t.hash} label="Copy infohash" mono />
        </Item>
        <Item label="Magnet">
          <Button
            size="xs"
            variant="outline"
            onClick={() => void copyText(t.magnet, "Copied the magnet link")}
          >
            <CopyIcon data-icon="inline-start" />
            Copy link
          </Button>
        </Item>
        <Item label="Private">{t.private ? "Yes" : "No"}</Item>
        <Item label="Added">{formatUnixTime(t.addedAt)}</Item>
        <Item label="Completed">{formatUnixTime(t.completedAt)}</Item>
        <Item label="Created">
          {formatUnixTime(t.createdAt)}
          {t.createdBy && <span className="text-muted-foreground"> by {t.createdBy}</span>}
        </Item>
        {t.comment && (
          <Item label="Comment">
            <Linkified text={t.comment} />
          </Item>
        )}
        <Item label="Save path">
          <Copyable text={t.savePath} label="Copy save path" mono />
        </Item>
        {t.incompletePath && (
          <Item label="Incomplete folder">
            <Copyable text={t.incompletePath} label="Copy incomplete folder" mono />
          </Item>
        )}
        <Item label="Content path">
          <Copyable text={t.contentPath} label="Copy content path" mono />
        </Item>
      </Section>

      <Section title="Options">
        <Item label="Category" htmlFor="detail-category">
          <CategorySelect
            id="detail-category"
            value={t.category}
            onChange={(category) => patch({ category })}
            className="w-56"
          />
        </Item>
        <Item label="Tags" htmlFor="detail-tags">
          <TagPicker
            id="detail-tags"
            label="Tags"
            value={t.tags ?? []}
            onChange={(tags) => patch({ tags })}
          />
        </Item>
        <SwitchItem
          label="Automatic management"
          description="The save path and incomplete folder follow the category."
          checked={t.autoManaged}
          onChange={(autoManaged) => patch({ autoManaged })}
        />
        <SwitchItem
          label="Download in order"
          checked={t.sequential}
          onChange={(sequential) => patch({ sequential })}
        />
        <SwitchItem
          label="First and last pieces first"
          checked={t.firstLastPiece}
          onChange={(firstLastPiece) => patch({ firstLastPiece })}
        />
        <SwitchItem
          label="Force start (ignore queue)"
          checked={t.forced}
          onChange={(forced) => patch({ forced })}
        />
        {/* A different torrent, or a change from elsewhere, starts the form over. */}
        <LimitsForm
          key={`${t.hash}:${t.ratioLimit}:${t.seedingTimeLimit}:${t.inactiveSeedingTimeLimit}:${t.seedGoalAction}:${t.seedGoalMode}`}
          torrent={t}
          onSave={patch}
          saving={update.isPending}
        />
      </Section>
    </div>
  );
}

// The default option's label says what the default comes to, which is
// known only while the torrent follows it.
function inherit(own: number, effective: number, kind: "ratio" | "minutes"): string {
  return own === -2 ? `Default (${formatShareLimit(effective, kind)})` : "Default";
}

function inheritOption(own: string, effective: string): string {
  return own === "default" ? `Default (${effective.toLowerCase()})` : "Default";
}

/**
 * Seeding goals, saved together. Default follows the category, then the
 * global settings; its label says what that comes to.
 */
function LimitsForm({
  torrent: t,
  onSave,
  saving,
}: {
  torrent: Torrent;
  onSave: (body: UpdateTorrentRequest) => void;
  saving: boolean;
}) {
  const current = {
    ratioLimit: t.ratioLimit,
    seedingTimeLimit: t.seedingTimeLimit,
    inactiveSeedingTimeLimit: t.inactiveSeedingTimeLimit,
    seedGoalAction: t.seedGoalAction,
    seedGoalMode: t.seedGoalMode,
  };
  const goals = t.seedGoals;
  const [draft, setDraft] = useState(current);
  const changed = (Object.keys(current) as (keyof typeof current)[]).filter(
    (k) => draft[k] !== current[k],
  );
  const set = <K extends keyof typeof current>(k: K, v: (typeof current)[K]) =>
    setDraft((d) => ({ ...d, [k]: v }));
  return (
    <form
      className="contents"
      onSubmit={(e) => {
        e.preventDefault();
        onSave(Object.fromEntries(changed.map((k) => [k, draft[k]])));
      }}
    >
      <Item label="Ratio goal" htmlFor="detail-ratio">
        <ShareLimitInput
          id="detail-ratio"
          kind="ratio"
          value={draft.ratioLimit}
          onChange={(v) => set("ratioLimit", v)}
          inheritLabel={inherit(t.ratioLimit, goals.ratioLimit, "ratio")}
        />
      </Item>
      <Item label="Seeding time goal" htmlFor="detail-seed-time">
        <ShareLimitInput
          id="detail-seed-time"
          kind="minutes"
          value={draft.seedingTimeLimit}
          onChange={(v) => set("seedingTimeLimit", v)}
          inheritLabel={inherit(t.seedingTimeLimit, goals.seedingTimeLimit, "minutes")}
        />
      </Item>
      <Item label="Inactive time goal" htmlFor="detail-inactive">
        <ShareLimitInput
          id="detail-inactive"
          kind="minutes"
          value={draft.inactiveSeedingTimeLimit}
          onChange={(v) => set("inactiveSeedingTimeLimit", v)}
          inheritLabel={inherit(
            t.inactiveSeedingTimeLimit,
            goals.inactiveSeedingTimeLimit,
            "minutes",
          )}
        />
      </Item>
      <Item label="Reached when" htmlFor="detail-goal-mode">
        <OptionSelect
          id="detail-goal-mode"
          value={draft.seedGoalMode}
          options={withDefault(
            seedGoalModes,
            inheritOption(t.seedGoalMode, optionLabel(seedGoalModes, goals.mode)),
          )}
          onChange={(v) => set("seedGoalMode", v)}
          className="w-56"
        />
      </Item>
      <Item label="Then" htmlFor="detail-goal-action">
        <OptionSelect
          id="detail-goal-action"
          value={draft.seedGoalAction}
          options={withDefault(
            seedGoalActions,
            inheritOption(t.seedGoalAction, optionLabel(seedGoalActions, goals.action)),
          )}
          onChange={(v) => set("seedGoalAction", v)}
          className="w-56"
        />
      </Item>
      {changed.length > 0 && (
        <div className="flex gap-2 sm:col-start-2">
          <Button type="submit" size="xs" disabled={saving}>
            Save goals
          </Button>
          <Button type="button" size="xs" variant="ghost" onClick={() => setDraft(current)}>
            Reset
          </Button>
        </div>
      )}
    </form>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="min-w-0">
      <h3 className="mb-2 text-xs font-semibold tracking-wider text-muted-foreground uppercase">
        {title}
      </h3>
      <dl className="grid grid-cols-1 gap-x-4 gap-y-2 sm:grid-cols-[minmax(8rem,auto)_1fr]">
        {children}
      </dl>
    </section>
  );
}

function Item({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <>
      <dt className="text-muted-foreground sm:pt-0.5">
        {htmlFor ? <label htmlFor={htmlFor}>{label}</label> : label}
      </dt>
      <dd className="min-w-0 tabular">{children}</dd>
    </>
  );
}

function SwitchItem({
  label,
  description,
  checked,
  onChange,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <>
      <dt className="text-muted-foreground" title={description}>
        {label}
      </dt>
      <dd>
        <Label className="font-normal normal-case">
          <Switch checked={checked} onCheckedChange={onChange} aria-label={label} />
          <span className="sr-only">{label}</span>
        </Label>
      </dd>
    </>
  );
}

function Copyable({ text, label, mono }: { text: string; label: string; mono?: boolean }) {
  return (
    <span className="flex min-w-0 items-center gap-1">
      <span className={mono ? "truncate font-mono text-xs" : "truncate"} title={text}>
        {text || "–"}
      </span>
      {text && (
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label={label}
          title={label}
          onClick={() => void copyText(text)}
        >
          <CopyIcon />
        </Button>
      )}
    </span>
  );
}

/** Text with http(s) links made clickable; torrent comments often hold one. */
function Linkified({ text }: { text: string }) {
  const parts = text.split(/(https?:\/\/[^\s]+)/g);
  return (
    <span className="break-words">
      {parts.map((p, i) =>
        /^https?:\/\//.test(p) ? (
          <a
            key={i}
            href={p}
            target="_blank"
            rel="noreferrer noopener"
            className="underline underline-offset-2"
          >
            {p}
          </a>
        ) : (
          p
        ),
      )}
    </span>
  );
}
