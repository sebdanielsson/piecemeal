import { useState } from "react";
import { Input } from "@/components/ui/input";
import { OptionSelect } from "./option-select";

type Mode = "global" | "unlimited" | "custom";

/**
 * A seeding goal: a ratio, or a time in minutes. Per torrent, -2 follows
 * the global setting and -1 is unlimited; globally only -1 exists.
 */
export function ShareLimitInput({
  id,
  value,
  onChange,
  kind,
  allowGlobal = true,
  disabled,
}: {
  id?: string;
  value: number;
  onChange: (value: number) => void;
  kind: "ratio" | "minutes";
  allowGlobal?: boolean;
  disabled?: boolean;
}) {
  const mode: Mode = value === -2 ? "global" : value < 0 ? "unlimited" : "custom";
  const [draft, setDraft] = useState(value >= 0 ? String(value) : "");
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    if (value >= 0 && Number(draft) !== value) {
      setDraft(String(value));
    }
  }
  const options = [
    ...(allowGlobal ? [{ value: "global" as const, label: "Global setting" }] : []),
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
        className="w-40"
        onChange={(m) => {
          if (m === "global") {
            onChange(-2);
          } else if (m === "unlimited") {
            onChange(-1);
          } else {
            const n = Number(draft);
            onChange(Number.isFinite(n) && n >= 0 && draft !== "" ? n : fallback);
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
            const n = Number(e.target.value.replace(",", "."));
            if (e.target.value !== "" && Number.isFinite(n) && n >= 0) {
              onChange(kind === "minutes" ? Math.round(n) : n);
            }
          }}
        />
      )}
    </div>
  );
}
