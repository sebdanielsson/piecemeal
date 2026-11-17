import { useState } from "react";
import type { Torrent } from "@/api/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { plural } from "@/lib/format";
import { useTorrentAction } from "@/lib/mutations";

/** Confirms removing torrents, optionally with their downloaded files. */
export function RemoveDialog({
  torrents,
  open,
  onOpenChange,
  onRemoved,
}: {
  torrents: Torrent[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRemoved?: () => void;
}) {
  const [deleteData, setDeleteData] = useState(false);
  const action = useTorrentAction();
  const shown = torrents.slice(0, 5);
  const remove = () => {
    action.mutate(
      { action: "remove", hashes: torrents.map((t) => t.hash), deleteData },
      {
        onSuccess: () => {
          onOpenChange(false);
          onRemoved?.();
        },
      },
    );
  };
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) {
          setDeleteData(false);
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            Remove {torrents.length === 1 ? "torrent" : plural(torrents.length, "torrent")}?
          </AlertDialogTitle>
          <AlertDialogDescription render={<div />}>
            <ul className="mt-1 flex flex-col gap-1 text-left">
              {shown.map((t) => (
                <li key={t.hash} className="truncate font-medium text-foreground" title={t.name}>
                  {t.name}
                </li>
              ))}
              {torrents.length > shown.length && (
                <li>and {plural(torrents.length - shown.length, "more torrent")}</li>
              )}
            </ul>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <Label className="flex items-center gap-3 font-normal normal-case">
          <Checkbox checked={deleteData} onCheckedChange={(v) => setDeleteData(v === true)} />
          Also delete the downloaded files
        </Label>
        <AlertDialogFooter>
          <AlertDialogCancel size="sm">Cancel</AlertDialogCancel>
          <AlertDialogAction
            size="sm"
            variant="destructive"
            onClick={remove}
            disabled={action.isPending || torrents.length === 0}
            autoFocus
          >
            {deleteData ? "Remove and delete files" : "Remove"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
