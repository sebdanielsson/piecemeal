import { useQuery } from "@tanstack/react-query";
import { PlusIcon, XIcon } from "lucide-react";
import { useState, type KeyboardEvent } from "react";
import { cn } from "cn";
import { tagsQuery } from "@/lib/queries";

/**
 * Picks tags: the existing ones as toggles, plus new ones typed in. New tags
 * are created by the server when a torrent gets them.
 */
export function TagPicker({
  id,
  value,
  onChange,
  disabled,
}: {
  id?: string;
  value: readonly string[];
  onChange: (tags: string[]) => void;
  disabled?: boolean;
}) {
  const { data: known = [] } = useQuery(tagsQuery);
  const [draft, setDraft] = useState("");
  const all = [...new Set([...known, ...value])].toSorted((a, b) => a.localeCompare(b));
  const toggle = (tag: string) =>
    onChange(value.includes(tag) ? value.filter((t) => t !== tag) : [...value, tag]);
  const addDraft = () => {
    const tag = draft.trim();
    if (tag && !value.includes(tag)) {
      onChange([...value, tag]);
    }
    setDraft("");
  };
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      addDraft();
    }
  };
  return (
    <div
      className="flex flex-wrap items-center gap-1.5"
      role="group"
      aria-labelledby={id ? `${id}-label` : undefined}
    >
      {all.map((tag) => {
        const on = value.includes(tag);
        return (
          <button
            key={tag}
            type="button"
            aria-pressed={on}
            disabled={disabled}
            onClick={() => toggle(tag)}
            className={cn(
              "flex h-7 items-center gap-1 border px-2 text-xs transition-colors focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none disabled:opacity-50",
              on
                ? "border-primary bg-primary text-primary-foreground"
                : "border-border text-muted-foreground hover:text-foreground",
            )}
          >
            {tag}
            {on && <XIcon className="size-3" aria-hidden="true" />}
          </button>
        );
      })}
      <span className="flex h-7 items-center border border-dashed border-border focus-within:border-ring">
        <input
          id={id}
          value={draft}
          disabled={disabled}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={addDraft}
          placeholder="New tag"
          aria-label="New tag"
          className="h-full w-24 bg-transparent px-2 text-xs outline-none placeholder:text-muted-foreground"
        />
        <button
          type="button"
          aria-label="Add tag"
          disabled={disabled || !draft.trim()}
          onClick={addDraft}
          className="flex h-full items-center px-1.5 text-muted-foreground hover:text-foreground disabled:opacity-40"
        >
          <PlusIcon className="size-3" />
        </button>
      </span>
    </div>
  );
}
