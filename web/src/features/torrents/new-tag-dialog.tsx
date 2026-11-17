import { useMutation } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { errorMessage, unwrap } from "@/lib/api";
import { plural } from "@/lib/format";

/** Creates a tag and adds it to the given torrents. */
export function NewTagDialog({
  hashes,
  open,
  onOpenChange,
}: {
  hashes: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: async (tag: string) => {
      await unwrap(api.PUT("/tags/{name}", { params: { path: { name: tag } } }));
      if (hashes.length > 0) {
        await unwrap(
          api.POST("/torrents/actions", { body: { action: "addTags", hashes, tags: [tag] } }),
        );
      }
    },
    onSuccess: () => {
      onOpenChange(false);
      setName("");
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const tag = name.trim();
    if (tag) {
      create.mutate(tag);
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>New tag</DialogTitle>
            {hashes.length > 0 && (
              <DialogDescription>
                It's added to {plural(hashes.length, "torrent")}.
              </DialogDescription>
            )}
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="new-tag">Name</FieldLabel>
            <Input id="new-tag" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          <DialogFooter>
            <Button type="submit" size="sm" disabled={!name.trim() || create.isPending}>
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
