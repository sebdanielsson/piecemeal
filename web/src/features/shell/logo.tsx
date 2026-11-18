import { cn } from "cn";

/** Four bars of growing height: pieces arriving. */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" className={cn("shrink-0", className)}>
      <rect width="32" height="32" className="fill-primary" />
      <path
        d="M6 22h4v4H6zM12 16h4v10h-4zM18 10h4v16h-4zM24 6h4v20h-4z"
        className="fill-primary-foreground"
      />
    </svg>
  );
}
