import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { RSSFeed } from "@/api/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { errorMessage, unwrap } from "@/lib/api";
import { keys } from "@/lib/queries";

/** Adds a feed, or changes one's name, URL and refresh interval. */
export function FeedDialog({
  feed,
  open,
  onOpenChange,
}: {
  feed?: RSSFeed;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const [edits, setEdits] = useState<{ url?: string; path?: string; minutes?: string }>({});
  const url = edits.url ?? feed?.url ?? "";
  const path = edits.path ?? feed?.path ?? "";
  const minutes = edits.minutes ?? (feed?.refreshInterval ? String(feed.refreshInterval / 60) : "");
  const close = (next: boolean) => {
    if (!next) {
      setEdits({});
    }
    onOpenChange(next);
  };
  const save = useMutation({
    mutationFn: async () => {
      const refreshInterval = minutes.trim() === "" ? 0 : Math.round(Number(minutes) * 60);
      if (!feed) {
        await unwrap(
          api.POST("/rss/feeds", { body: { url: url.trim(), path: path.trim(), refreshInterval } }),
        );
        return;
      }
      await unwrap(
        api.PATCH("/rss/feeds/{uid}", {
          params: { path: { uid: feed.uid } },
          body: {
            url: url.trim() !== feed.url ? url.trim() : undefined,
            path: path.trim() !== feed.path ? path.trim() : undefined,
            refreshInterval: refreshInterval !== feed.refreshInterval ? refreshInterval : undefined,
          },
        }),
      );
    },
    onSuccess: () => {
      close(false);
      return qc.invalidateQueries({ queryKey: keys.rss });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const badMinutes = minutes.trim() !== "" && !(Number(minutes) > 0);
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>{feed ? "Edit feed" : "Add feed"}</DialogTitle>
            <DialogDescription>
              Private trackers' feeds usually need the passkey in the URL; cookies set in the
              qBittorrent API are sent too.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="feed-url">URL</FieldLabel>
              <Input
                id="feed-url"
                value={url}
                onChange={(e) => setEdits((s) => ({ ...s, url: e.target.value }))}
                className="font-mono text-xs"
                autoFocus
                required
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="feed-path">Name</FieldLabel>
              <Input
                id="feed-path"
                value={path}
                onChange={(e) => setEdits((s) => ({ ...s, path: e.target.value }))}
                placeholder="The feed's title"
              />
              <FieldDescription>Use \ to put it in a folder, which must exist.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="feed-minutes">Refresh every (minutes)</FieldLabel>
              <Input
                id="feed-minutes"
                inputMode="decimal"
                value={minutes}
                onChange={(e) => setEdits((s) => ({ ...s, minutes: e.target.value }))}
                placeholder="As in the RSS settings"
                aria-invalid={badMinutes || undefined}
                className="max-w-40"
              />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" size="sm" disabled={!url.trim() || badMinutes || save.isPending}>
              {feed ? "Save" : "Add"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
