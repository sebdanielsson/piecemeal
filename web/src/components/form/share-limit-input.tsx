import { useState } from "react";
import { Input } from "@/components/ui/input";
import { OptionSelect } from "./option-select";

type Mode = "global" | "unlimited" | "custom";

/**
 * Reads a typed seeding goal: a ratio, which may use a decimal comma, or
 * whole minutes. Returns null for anything that isn't a goal yet.
 */
export function parseShareLimit(text: string, kind: "ratio" | "minutes"): number | null {
  const trimmed = text.trim();
  const n = Number(trimmed.replace(",", "."));
  if (trimmed === "" || !Number.isFinite(n) || n < 0) {
    return null;
  }
  return kind === "minutes" ? Math.round(n) : n;
}

/**
 * A seeding goal: a ratio, or a time in minutes. Per torrent and per
 * category, -2 follows the next level (labelled inheritLabel) and -1 is
 * unlimited; globally only -1 exists.
 */
export function ShareLimitInput({
  id,
  value,
  onChange,
  kind,
  allowGlobal = true,
  inheritLabel = "Global setting",
  disabled,
}: {
  id?: string;
  value: number;
  onChange: (value: number) => void;
  kind: "ratio" | "minutes";
  allowGlobal?: boolean;
  inheritLabel?: string;
  disabled?: boolean;
}) {
  const mode: Mode = value === -2 ? "global" : value < 0 ? "unlimited" : "custom";
  const [draft, setDraft] = useState(value >= 0 ? String(value) : "");
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    // Only replace what's typed when it means something else, so "1,5"
    // isn't rewritten to "1.5" while typing.
    if (value >= 0 && parseShareLimit(draft, kind) !== value) {
      setDraft(String(value));
    }
  }
  const options = [
    ...(allowGlobal ? [{ value: "global" as const, label: inheritLabel }] : []),
    { value: "unlimited" as const, label: "Unlimited" },
    { value: "custom" as const, label: kind === "ratio" ? "Ratio" : "Minutes" },
  ];
  const fallback = kind === "ratio" ? 2 : 1440;
  return (
    <div className="flex items-end gap-2">
      <OptionSelect
        id={id}
        value={mode}
        options={options}
        disabled={disabled}
        className="w-44"
        onChange={(m) => {
          if (m === "global") {
            onChange(-2);
          } else if (m === "unlimited") {
            onChange(-1);
          } else {
            onChange(parseShareLimit(draft, kind) ?? fallback);
          }
        }}
      />
      {mode === "custom" && (
        <Input
          aria-label={kind === "ratio" ? "Ratio" : "Minutes"}
          inputMode={kind === "ratio" ? "decimal" : "numeric"}
          value={draft}
          disabled={disabled}
          className="w-28 tabular"
          onChange={(e) => {
            setDraft(e.target.value);
            const n = parseShareLimit(e.target.value, kind);
            if (n !== null) {
              onChange(n);
            }
          }}
        />
      )}
    </div>
  );
}
