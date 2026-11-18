import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PauseIcon, PlayIcon, Trash2Icon } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { cn } from "cn";
import { api } from "@/api/client";
import type { LogEntry } from "@/api/types";
import { OptionSelect } from "@/components/form/option-select";
import { Button } from "@/components/ui/button";
import { unwrap } from "@/lib/api";
import { keys } from "@/lib/queries";
import { Page } from "../shell/page";

type Level = "INFO" | "WARN" | "ERROR";
const maxEntries = 2000;

/** Appends new entries, dropping duplicates and the oldest beyond the cap. */
export function appendLog(
  prev: readonly LogEntry[],
  next: readonly LogEntry[],
  cap = maxEntries,
): LogEntry[] {
  const last = prev.at(-1)?.id ?? -1;
  const fresh = next.filter((e) => e.id > last);
  if (fresh.length === 0) {
    return prev as LogEntry[];
  }
  const all = [...prev, ...fresh];
  return all.length > cap ? all.slice(all.length - cap) : all;
}

const levelClass: Record<LogEntry["level"], string> = {
  DEBUG: "text-muted-foreground",
  INFO: "text-state-progress",
  WARN: "text-state-warning",
  ERROR: "text-destructive",
};

export function LogPage() {
  const qc = useQueryClient();
  const [level, setLevel] = useState<Level>("INFO");
  const [paused, setPaused] = useState(false);
  const [follow, setFollow] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const queryKey = [...keys.log, level];

  // The cached list grows with each poll, which asks only for newer entries.
  const poll = useQuery({
    queryKey,
    queryFn: async (): Promise<LogEntry[]> => {
      const prev = qc.getQueryData<LogEntry[]>(queryKey) ?? [];
      const after = prev.at(-1)?.id;
      const res = await unwrap(api.GET("/log", { params: { query: { level, after } } }));
      return appendLog(prev, res.items ?? []);
    },
    refetchInterval: paused ? false : 2_000,
    gcTime: 0,
  });
  // Clearing hides what is there; polling carries on from the last entry.
  const [hiddenUpTo, setHiddenUpTo] = useState(0);
  const entries = (poll.data ?? []).filter((e) => e.id > hiddenUpTo);

  const newest = entries.at(-1)?.id;
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && follow && newest !== undefined) {
      el.scrollTop = el.scrollHeight;
    }
  }, [newest, follow]);

  return (
    <Page
      title="Log"
      description="Recent log entries from piecemeal, updated every two seconds."
      className="max-w-none"
      actions={
        <>
          <OptionSelect
            aria-label="Minimum level"
            value={level}
            onChange={setLevel}
            options={[
              { value: "INFO", label: "Info and up" },
              { value: "WARN", label: "Warnings and errors" },
              { value: "ERROR", label: "Errors only" },
            ]}
            className="w-44 text-xs"
          />
          <Button
            size="sm"
            variant="outline"
            onClick={() => setPaused(!paused)}
            aria-pressed={paused}
          >
            {paused ? (
              <PlayIcon data-icon="inline-start" />
            ) : (
              <PauseIcon data-icon="inline-start" />
            )}
            {paused ? "Resume" : "Pause"}
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="Clear the view"
            title="Clear the view"
            onClick={() => setHiddenUpTo(poll.data?.at(-1)?.id ?? 0)}
          >
            <Trash2Icon />
          </Button>
        </>
      }
    >
      {poll.isError && (
        <p className="text-destructive">Couldn't load the log: {poll.error.message}</p>
      )}
      <div
        ref={scrollRef}
        onScroll={(e) => {
          const el = e.currentTarget;
          setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
        }}
        className="h-[calc(100svh-16rem)] min-h-64 overflow-auto border bg-muted/30 p-2 font-mono text-xs"
        role="log"
        aria-live="off"
        aria-label="Log entries"
      >
        {entries.length === 0 ? (
          <p className="p-2 text-muted-foreground">No entries yet.</p>
        ) : (
          entries.map((e) => (
            <div key={e.id} className="flex gap-3 py-0.5 hover:bg-muted">
              <time className="shrink-0 text-muted-foreground" dateTime={e.time}>
                {new Date(e.time).toLocaleTimeString()}
              </time>
              <span className={cn("w-11 shrink-0 font-semibold", levelClass[e.level])}>
                {e.level}
              </span>
              <span className="min-w-0 break-words whitespace-pre-wrap">
                {e.message}
                {e.attrs && <span className="text-muted-foreground"> {e.attrs}</span>}
              </span>
            </div>
          ))
        )}
      </div>
    </Page>
  );
}
