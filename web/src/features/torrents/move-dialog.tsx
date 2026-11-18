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
import { useTorrentAction } from "@/lib/mutations";

/** Moves the selected torrents' data to another folder. */
export function MoveDialog({
  torrents,
  open,
  onOpenChange,
}: {
  torrents: Torrent[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const shared = torrents.every((t) => t.savePath === torrents[0]?.savePath)
    ? (torrents[0]?.savePath ?? "")
    : "";
  const [path, setPath] = useState<string | null>(null);
  const value = path ?? shared;
  const action = useTorrentAction();
  const close = (next: boolean) => {
    if (!next) {
      setPath(null);
    }
    onOpenChange(next);
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    action.mutate(
      { action: "move", hashes: torrents.map((t) => t.hash), savePath: value.trim() },
      { onSuccess: () => close(false) },
    );
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Move data</DialogTitle>
            <DialogDescription>
              Moves the files of {plural(torrents.length, "torrent")}. They keep running from the
              new folder once the move is done.
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
            <FieldDescription>An absolute path inside the allowed folders.</FieldDescription>
          </Field>
          <DialogFooter>
            <Button
              type="submit"
              size="sm"
              disabled={!value.trim() || value.trim() === shared || action.isPending}
            >
              Move
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
