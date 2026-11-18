import { useState, type FormEvent } from "react";
import type { Torrent } from "@/api/types";
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
import { Input } from "@/components/ui/input";
import { plural } from "@/lib/format";
import { useTorrentAction, type TorrentActionInput } from "@/lib/mutations";

/** The value every torrent shares, or "" when they differ. */
function sharedValue(torrents: Torrent[], get: (t: Torrent) => string): string {
  const first = torrents[0];
  return first && torrents.every((t) => get(t) === get(first)) ? get(first) : "";
}

/**
 * Moves the selected torrents' data to another save path, or changes their
 * incomplete folder. Either turns automatic management off.
 */
export function MoveDialog({
  torrents,
  open,
  onOpenChange,
}: {
  torrents: Torrent[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const sharedSave = sharedValue(torrents, (t) => t.savePath);
  const sharedIncomplete = sharedValue(torrents, (t) => t.incompletePath);
  const [path, setPath] = useState<string | null>(null);
  const [incomplete, setIncomplete] = useState<string | null>(null);
  const value = path ?? sharedSave;
  const incompleteValue = incomplete ?? sharedIncomplete;
  const auto = torrents.some((t) => t.autoManaged);
  const action = useTorrentAction();
  const close = (next: boolean) => {
    if (!next) {
      setPath(null);
      setIncomplete(null);
    }
    onOpenChange(next);
  };
  const saveChanged = value.trim() !== "" && value.trim() !== sharedSave;
  const incompleteChanged = incomplete !== null && incompleteValue.trim() !== sharedIncomplete;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const hashes = torrents.map((t) => t.hash);
    const actions: TorrentActionInput[] = [];
    if (saveChanged) {
      actions.push({ action: "move", hashes, savePath: value.trim() });
    }
    if (incompleteChanged) {
      actions.push({ action: "setIncompletePath", hashes, incompletePath: incompleteValue.trim() });
    }
    try {
      for (const a of actions) {
        await action.mutateAsync(a);
      }
      close(false);
    } catch {
      // The mutation shows the error; the dialog stays open.
    }
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <form onSubmit={(e) => void submit(e)} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Move data</DialogTitle>
            <DialogDescription>
              Moves the files of {plural(torrents.length, "torrent")}. They keep running from the
              new folder once the move is done.
              {auto && " Automatic management turns off."}
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="move-path">Save path</FieldLabel>
            <Input
              id="move-path"
              value={value}
              onChange={(e) => setPath(e.target.value)}
              className="font-mono"
              autoFocus
            />
            <FieldDescription>
              An absolute path inside the allowed folders. Incomplete torrents in their incomplete
              folder move there once complete.
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="move-incomplete">Incomplete folder</FieldLabel>
            <Input
              id="move-incomplete"
              value={incompleteValue}
              onChange={(e) => setIncomplete(e.target.value)}
              placeholder="None"
              className="font-mono"
            />
            <FieldDescription>
              Where incomplete torrents are kept until they complete. Empty keeps them in their save
              path.
            </FieldDescription>
          </Field>
          <DialogFooter>
            <Button
              type="submit"
              size="sm"
              disabled={(!saveChanged && !incompleteChanged) || action.isPending}
            >
              Move
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
