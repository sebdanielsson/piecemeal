import { useState, type FormEvent } from "react";
import type { Tracker } from "@/api/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { formatTiers, parseTiers } from "@/lib/trackers";
import { useSetTrackers } from "@/lib/mutations";

/** Edits a torrent's trackers, tier by tier, and its web seeds. */
export function EditTrackersDialog({
  hash,
  trackers,
  webSeeds,
  open,
  onOpenChange,
}: {
  hash: string;
  trackers: Tracker[];
  webSeeds: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [tiers, setTiers] = useState<string | null>(null);
  const [seeds, setSeeds] = useState<string | null>(null);
  const save = useSetTrackers(hash);
  const close = (next: boolean) => {
    if (!next) {
      setTiers(null);
      setSeeds(null);
    }
    onOpenChange(next);
  };
  const tiersText = tiers ?? formatTiers(trackers);
  const seedsText = seeds ?? webSeeds.join("\n");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(
      {
        tiers: parseTiers(tiersText),
        webSeeds: seedsText
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean),
      },
      { onSuccess: () => close(false) },
    );
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Edit trackers</DialogTitle>
            <DialogDescription>
              One URL per line. A blank line starts a new tier; trackers in a tier are tried in
              turn, and every tier is announced to.
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="edit-trackers">Trackers</FieldLabel>
            <Textarea
              id="edit-trackers"
              value={tiersText}
              onChange={(e) => setTiers(e.target.value)}
              rows={8}
              className="font-mono text-xs"
              autoFocus
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="edit-web-seeds">Web seeds</FieldLabel>
            <Textarea
              id="edit-web-seeds"
              value={seedsText}
              onChange={(e) => setSeeds(e.target.value)}
              rows={3}
              className="font-mono text-xs"
            />
            <FieldDescription>HTTP servers that have the files (BEP 19).</FieldDescription>
          </Field>
          <DialogFooter>
            <Button type="submit" size="sm" disabled={save.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
