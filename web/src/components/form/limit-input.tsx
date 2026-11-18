import { useState } from "react";
import { cn } from "cn";
import { Input } from "@/components/ui/input";
import { OptionSelect } from "./option-select";

type Unit = "KiB" | "MiB";
const factor: Record<Unit, number> = { KiB: 1024, MiB: 1024 * 1024 };

/** Splits a limit in bytes per second into a number and a unit for editing. */
export function limitParts(bytesPerSecond: number): { amount: string; unit: Unit } {
  if (bytesPerSecond <= 0) {
    return { amount: "", unit: "KiB" };
  }
  // Whole tenths of a MiB/s read better in MiB/s.
  const tenths = (bytesPerSecond * 10) / factor.MiB;
  if (bytesPerSecond >= factor.MiB && Math.abs(tenths - Math.round(tenths)) < 1e-9) {
    return { amount: String(Math.round(tenths) / 10), unit: "MiB" };
  }
  return { amount: String(Math.round(bytesPerSecond / factor.KiB)), unit: "KiB" };
}

/** Bytes per second from an edited amount; empty or 0 is unlimited. */
export function limitFromParts(amount: string, unit: Unit): number {
  const n = Number(amount.trim().replace(",", "."));
  if (!Number.isFinite(n) || n <= 0) {
    return 0;
  }
  return Math.round(n * factor[unit]);
}

/** A speed limit in bytes per second, edited as KiB/s or MiB/s. 0 is unlimited. */
export function LimitInput({
  id,
  value,
  onChange,
  disabled,
  className,
}: {
  id?: string;
  value: number;
  onChange: (bytesPerSecond: number) => void;
  disabled?: boolean;
  className?: string;
}) {
  const [parts, setParts] = useState(() => limitParts(value));
  // Follow outside changes, unless they are just our own edit coming back.
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    if (limitFromParts(parts.amount, parts.unit) !== value) {
      setParts(limitParts(value));
    }
  }
  const update = (amount: string, unit: Unit) => {
    setParts({ amount, unit });
    onChange(limitFromParts(amount, unit));
  };
  return (
    <div className={cn("flex items-end gap-2", className)}>
      <Input
        id={id}
        inputMode="decimal"
        placeholder="Unlimited"
        value={parts.amount}
        disabled={disabled}
        onChange={(e) => update(e.target.value, parts.unit)}
        className="min-w-0 flex-1 tabular"
      />
      <OptionSelect
        aria-label="Unit"
        value={parts.unit}
        onChange={(unit) => update(parts.amount, unit)}
        options={[
          { value: "KiB", label: "KiB/s" },
          { value: "MiB", label: "MiB/s" },
        ]}
        disabled={disabled}
        className="w-20"
      />
    </div>
  );
}
