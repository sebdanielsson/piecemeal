import { useState, type FormEvent } from "react";
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
import { useRenamePath } from "@/lib/mutations";

/** Renames one of a torrent's files or folders, on disk too. */
export function RenamePathDialog({
  hash,
  path,
  folder,
  onClose,
}: {
  hash: string;
  /** The current path, relative to the save path; undefined when closed. */
  path: string | undefined;
  folder: boolean;
  onClose: () => void;
}) {
  const [value, setValue] = useState<string | null>(null);
  const rename = useRenamePath(hash);
  const current = value ?? path ?? "";
  const close = () => {
    setValue(null);
    onClose();
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (path) {
      rename.mutate({ oldPath: path, newPath: current.trim() }, { onSuccess: close });
    }
  };
  return (
    <Dialog open={path !== undefined} onOpenChange={(open) => !open && close()}>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Rename {folder ? "folder" : "file"}</DialogTitle>
            <DialogDescription>
              The {folder ? "folder" : "file"} is renamed on disk too. Use / to move it into another
              folder.
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="rename-path">Path</FieldLabel>
            <Input
              id="rename-path"
              value={current}
              onChange={(e) => setValue(e.target.value)}
              className="font-mono"
              autoFocus
            />
            <FieldDescription>Relative to the torrent's save path.</FieldDescription>
          </Field>
          <DialogFooter>
            <Button
              type="submit"
              size="sm"
              disabled={!current.trim() || current.trim() === path || rename.isPending}
            >
              Rename
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
