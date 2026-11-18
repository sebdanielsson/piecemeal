import { useQuery } from "@tanstack/react-query";
import { LockIcon } from "lucide-react";
import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { toast } from "sonner";
import type { Settings, TransferLimits } from "@/api/types";
import { LimitInput } from "@/components/form/limit-input";
import { OptionSelect } from "@/components/form/option-select";
import { ShareLimitInput } from "@/components/form/share-limit-input";
import { Button } from "@/components/ui/button";
import { Field, FieldContent, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { transferSettingKeys, useUpdateSettings, useUpdateTransfer } from "@/lib/mutations";
import { meQuery, settingsQuery } from "@/lib/queries";
import { PageSection } from "../shell/page";

type Key = keyof Settings;

export type FieldDef =
  | { key: Key; label: string; description?: string; kind: "text" | "path" | "bool" | "limit" }
  | { key: Key; label: string; description?: string; kind: "int"; min?: number; max?: number }
  | { key: Key; label: string; description?: string; kind: "ratio" | "minutes" }
  | {
      key: Key;
      label: string;
      description?: string;
      kind: "select";
      options: { value: string; label: string }[];
    };

export interface FieldGroupDef {
  title: string;
  description?: string;
  fields: FieldDef[];
}

/** The changed fields between two settings objects. */
export function settingsDiff(base: Settings, draft: Settings): Partial<Settings> {
  const out: Partial<Settings> = {};
  for (const k of Object.keys(draft) as Key[]) {
    if (draft[k] !== base[k]) {
      (out as Record<string, unknown>)[k] = draft[k];
    }
  }
  return out;
}

function withoutKeys<T extends object>(obj: T, keys: string[]): T {
  const out = { ...obj } as Record<string, unknown>;
  for (const k of keys) {
    delete out[k];
  }
  return out as T;
}

/**
 * Splits changed settings into the speed limits, which PATCH /transfer takes
 * from any signed-in user, and the rest, which only admins may change.
 */
export function splitSettingsPatch(changes: Partial<Settings>): {
  transfer: TransferLimits;
  settings: Partial<Settings>;
} {
  const transfer: Record<string, unknown> = {};
  const settings: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(changes)) {
    (transferSettingKeys.has(k as Key) ? transfer : settings)[k] = v;
  }
  return { transfer: transfer as TransferLimits, settings: settings as Partial<Settings> };
}

/**
 * Edits groups of settings with one save bar. Settings pinned by an
 * environment variable are read-only, and so is everything but the speed
 * limits for non-admins.
 */
export function SettingsForm({ groups, footer }: { groups: FieldGroupDef[]; footer?: ReactNode }) {
  const { data, isPending, isError, error } = useQuery(settingsQuery);
  const { data: me } = useQuery(meQuery);
  const updateSettings = useUpdateSettings();
  const updateTransfer = useUpdateTransfer();
  const saving = updateSettings.isPending || updateTransfer.isPending;
  const base = data?.settings;
  // Only the user's own edits are kept, on top of the current settings, so
  // changes made elsewhere (the transfer bar, another browser) show up and
  // aren't reverted by saving. An edit back to the current value no longer
  // counts as a change.
  const [edits, setEdits] = useState<Partial<Settings>>({});
  const draft = useMemo(() => base && { ...base, ...edits }, [base, edits]);

  const keysHere = useMemo(
    () => new Set(groups.flatMap((g) => g.fields.map((f) => f.key))),
    [groups],
  );
  const changes = useMemo(() => {
    if (!base || !draft) {
      return {};
    }
    const diff = settingsDiff(base, draft);
    return Object.fromEntries(
      Object.entries(diff).filter(([k]) => keysHere.has(k as Key)),
    ) as Partial<Settings>;
  }, [base, draft, keysHere]);
  const dirty = Object.keys(changes).length > 0;

  if (isPending) {
    return (
      <div className="flex justify-center p-6">
        <Spinner />
      </div>
    );
  }
  if (isError || !data || !draft) {
    return <p className="text-destructive">Couldn't load the settings: {error?.message}</p>;
  }
  const admin = me?.isAdmin === true;
  const editable = (key: Key) => admin || transferSettingKeys.has(key);
  const fields = groups.flatMap((g) => g.fields);
  const canSave = fields.some((f) => editable(f.key) && data.locked[f.key] === undefined);
  const adminOnly = !admin && fields.some((f) => !transferSettingKeys.has(f.key));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const patch = splitSettingsPatch(changes);
    try {
      // The saved values come back through the settings cache.
      if (Object.keys(patch.settings).length > 0) {
        await updateSettings.mutateAsync(patch.settings);
        setEdits((prev) => withoutKeys(prev, Object.keys(patch.settings)));
      }
      if (Object.keys(patch.transfer).length > 0) {
        await updateTransfer.mutateAsync(patch.transfer);
        setEdits((prev) => withoutKeys(prev, Object.keys(patch.transfer)));
      }
      toast.success("Settings saved");
    } catch {
      // The mutation has shown the error.
    }
  };

  return (
    <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-10">
      {adminOnly && (
        <p className="border bg-muted/50 p-3 text-sm text-muted-foreground">
          Only admins can change these settings
          {fields.some((f) => transferSettingKeys.has(f.key)) ? ", except the speed limits." : "."}
        </p>
      )}
      {groups.map((g) => (
        <PageSection key={g.title} title={g.title} description={g.description}>
          <div className="flex flex-col gap-6">
            {g.fields.map((f) => (
              <SettingField
                key={f.key}
                def={f}
                value={draft[f.key]}
                lockedBy={data.locked[f.key]}
                disabled={!editable(f.key)}
                onChange={(v) => setEdits((e) => ({ ...e, [f.key]: v }))}
              />
            ))}
          </div>
        </PageSection>
      ))}
      {footer}
      {canSave && (
        <div className="sticky bottom-0 -mx-4 flex items-center justify-end gap-2 border-t bg-background/95 px-4 py-3 backdrop-blur sm:-mx-6 sm:px-6">
          <span className="mr-auto text-xs text-muted-foreground" aria-live="polite">
            {dirty
              ? `${Object.keys(changes).length} unsaved ${Object.keys(changes).length === 1 ? "change" : "changes"}`
              : "All changes saved"}
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={!dirty}
            onClick={() => setEdits({})}
          >
            Reset
          </Button>
          <Button type="submit" size="sm" disabled={!dirty || saving}>
            {saving && <Spinner />}
            Save
          </Button>
        </div>
      )}
    </form>
  );
}

function SettingField({
  def,
  value,
  lockedBy,
  disabled,
  onChange,
}: {
  def: FieldDef;
  value: Settings[Key];
  lockedBy: string | undefined;
  disabled: boolean;
  onChange: (value: Settings[Key]) => void;
}) {
  const id = `setting-${def.key}`;
  const off = disabled || lockedBy !== undefined;
  const description = (
    <>
      {def.description}
      {lockedBy && (
        <span className="mt-1 flex items-center gap-1 text-xs">
          <LockIcon className="size-3" />
          Set by <code className="font-mono">{lockedBy}</code>
        </span>
      )}
    </>
  );

  if (def.kind === "bool") {
    return (
      <Field orientation="horizontal" className="items-start justify-between gap-6">
        <FieldContent>
          <FieldLabel htmlFor={id}>{def.label}</FieldLabel>
          <FieldDescription>{description}</FieldDescription>
        </FieldContent>
        <Switch
          id={id}
          checked={value === true}
          onCheckedChange={(v) => onChange(v)}
          disabled={off}
        />
      </Field>
    );
  }

  let control: ReactNode;
  switch (def.kind) {
    case "limit":
      control = (
        <LimitInput
          id={id}
          value={Number(value)}
          onChange={onChange}
          disabled={off}
          className="max-w-64"
        />
      );
      break;
    case "ratio":
    case "minutes":
      control = (
        <ShareLimitInput
          id={id}
          kind={def.kind}
          value={Number(value)}
          onChange={onChange}
          allowGlobal={false}
          disabled={off}
        />
      );
      break;
    case "int":
      control = (
        <IntInput
          id={id}
          value={Number(value)}
          onChange={onChange}
          disabled={off}
          min={def.min}
          max={def.max}
        />
      );
      break;
    case "select":
      control = (
        <OptionSelect
          id={id}
          value={String(value)}
          options={def.options}
          onChange={onChange}
          disabled={off}
          className="max-w-64"
        />
      );
      break;
    default:
      control = (
        <Input
          id={id}
          value={String(value)}
          disabled={off}
          onChange={(e) => onChange(e.target.value)}
          className={def.kind === "path" ? "max-w-xl font-mono text-xs" : "max-w-xl"}
        />
      );
  }
  return (
    <Field>
      <FieldLabel htmlFor={id}>{def.label}</FieldLabel>
      {control}
      {(def.description || lockedBy) && <FieldDescription>{description}</FieldDescription>}
    </Field>
  );
}

/** An integer field that can be cleared while typing. */
function IntInput({
  id,
  value,
  onChange,
  disabled,
  min,
  max,
}: {
  id: string;
  value: number;
  onChange: (value: number) => void;
  disabled?: boolean;
  min?: number;
  max?: number;
}) {
  const [text, setText] = useState(String(value));
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    if (Number(text) !== value || text === "") {
      setText(String(value));
    }
  }
  const n = Number(text);
  const invalid =
    text === "" ||
    !Number.isInteger(n) ||
    (min !== undefined && n < min) ||
    (max !== undefined && n > max);
  return (
    <Input
      id={id}
      inputMode="numeric"
      value={text}
      disabled={disabled}
      aria-invalid={invalid || undefined}
      className="max-w-40 tabular"
      onChange={(e) => {
        setText(e.target.value);
        const v = Number(e.target.value);
        if (
          e.target.value !== "" &&
          Number.isInteger(v) &&
          (min === undefined || v >= min) &&
          (max === undefined || v <= max)
        ) {
          onChange(v);
        }
      }}
    />
  );
}
