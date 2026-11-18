import type { Settings, Torrent } from "@/api/types";
import type { Option } from "./option-select";

/** A torrent's or category's seed goal action; default follows the next level. */
export type SeedGoalAction = Torrent["seedGoalAction"];
/** A torrent's or category's seed goal mode; default follows the next level. */
export type SeedGoalMode = Torrent["seedGoalMode"];

export const seedGoalActions: Option<Settings["seedGoalAction"]>[] = [
  { value: "stop", label: "Stop the torrent" },
  { value: "remove", label: "Remove the torrent" },
  { value: "removeWithData", label: "Remove the torrent and its files" },
];

export const seedGoalModes: Option<Settings["seedGoalMode"]>[] = [
  { value: "any", label: "Any goal is met" },
  { value: "all", label: "Every goal set is met" },
];

/** Options with a first one that follows the default, labelled label. */
export function withDefault<T extends string>(
  options: Option<T>[],
  label: string,
): Option<T | "default">[] {
  return [{ value: "default", label }, ...options];
}

/** The label of an option's value, for describing what a default resolves to. */
export function optionLabel<T extends string>(options: Option<T>[], value: T): string {
  return options.find((o) => o.value === value)?.label ?? value;
}
