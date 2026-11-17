import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { PencilIcon, PlusIcon, TagIcon, Trash2Icon } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { Category } from "@/api/types";
import { DataTable, type DataColumn } from "@/components/data/data-table";
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
import { countTorrents, type FilterCounts } from "@/lib/filters";
import { plural } from "@/lib/format";
import type { TorrentMap } from "@/lib/live/reducer";
import { categoriesQuery, keys, settingsQuery, tagsQuery, torrentsQuery } from "@/lib/queries";
import { Page, PageSection } from "../shell/page";

const selectCounts = (m: TorrentMap) => countTorrents(m.values());

function makeCategoryColumns(
  counts: FilterCounts | undefined,
  onEdit: (c: Category) => void,
  onDelete: (name: string) => void,
): DataColumn<Category>[] {
  return [
    {
      id: "name",
      header: "Name",
      cell: (c) => (
        <Link to="/" search={{ category: c.name }} className="font-medium hover:underline">
          {c.name}
        </Link>
      ),
      sort: (c) => c.name,
    },
    {
      id: "path",
      header: "Save path",
      cell: (c) =>
        c.savePath ? (
          <span className="font-mono text-xs">{c.savePath}</span>
        ) : (
          <span className="text-muted-foreground">Default</span>
        ),
      sort: (c) => c.savePath,
    },
    {
      id: "count",
      header: "Torrents",
      align: "right",
      cell: (c) => (counts?.categories.get(c.name) ?? 0).toLocaleString(),
      sort: (c) => counts?.categories.get(c.name) ?? 0,
    },
    {
      id: "actions",
      header: "",
      className: "w-20 text-right",
      cell: (c) => (
        <span className="flex justify-end">
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label={`Edit ${c.name}`}
            onClick={() => onEdit(c)}
          >
            <PencilIcon />
          </Button>
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label={`Delete ${c.name}`}
            onClick={() => onDelete(c.name)}
          >
            <Trash2Icon />
          </Button>
        </span>
      ),
    },
  ];
}

export function CategoriesPage() {
  const { data: categories = [] } = useQuery(categoriesQuery);
  const { data: tags = [] } = useQuery(tagsQuery);
  const { data: counts } = useQuery({ ...torrentsQuery, select: selectCounts });
  const [editing, setEditing] = useState<Category | "new" | null>(null);
  const [deleting, setDeleting] = useState<{ kind: "category" | "tag"; name: string } | null>(null);
  const [newTag, setNewTag] = useState("");
  const qc = useQueryClient();

  const createTag = useMutation({
    mutationFn: (name: string) => unwrap(api.PUT("/tags/{name}", { params: { path: { name } } })),
    onSuccess: (_d, name) => {
      setNewTag("");
      qc.setQueryData<string[]>(keys.tags, (prev) =>
        [...new Set([...(prev ?? []), name])].toSorted(),
      );
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const categoryColumns = useMemo(
    () =>
      makeCategoryColumns(counts, setEditing, (name) => setDeleting({ kind: "category", name })),
    [counts],
  );

  return (
    <Page
      title="Categories & tags"
      description="Categories decide where torrents are saved; tags are free-form labels."
    >
      <PageSection
        title="Categories"
        actions={
          <Button size="xs" onClick={() => setEditing("new")}>
            <PlusIcon data-icon="inline-start" />
            New category
          </Button>
        }
      >
        <DataTable
          rows={categories}
          columns={categoryColumns}
          rowKey={(c) => c.name}
          initialSort={{ id: "name", desc: false }}
          empty="No categories yet."
          label="Categories"
        />
      </PageSection>

      <PageSection title="Tags">
        <form
          className="flex max-w-sm items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (newTag.trim()) {
              createTag.mutate(newTag.trim());
            }
          }}
        >
          <Field className="flex-1">
            <FieldLabel htmlFor="new-tag-name">New tag</FieldLabel>
            <Input id="new-tag-name" value={newTag} onChange={(e) => setNewTag(e.target.value)} />
          </Field>
          <Button type="submit" size="sm" disabled={!newTag.trim() || createTag.isPending}>
            Add
          </Button>
        </form>
        {tags.length === 0 ? (
          <p className="text-sm text-muted-foreground">No tags yet.</p>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {tags.map((t) => (
              <li key={t} className="flex items-center border">
                <Link
                  to="/"
                  search={{ tag: t }}
                  className="flex items-center gap-1.5 px-2.5 py-1 text-sm hover:bg-muted"
                >
                  <TagIcon className="size-3.5 text-muted-foreground" />
                  {t}
                  <span className="text-xs text-muted-foreground tabular">
                    {counts?.tags.get(t) ?? 0}
                  </span>
                </Link>
                <button
                  type="button"
                  aria-label={`Delete tag ${t}`}
                  className="border-l px-1.5 py-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                  onClick={() => setDeleting({ kind: "tag", name: t })}
                >
                  <Trash2Icon className="size-3.5" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </PageSection>

      {editing && (
        <CategoryDialog
          category={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
        />
      )}
      <DeleteDialog
        target={deleting}
        count={
          deleting
            ? deleting.kind === "category"
              ? (counts?.categories.get(deleting.name) ?? 0)
              : (counts?.tags.get(deleting.name) ?? 0)
            : 0
        }
        onClose={() => setDeleting(null)}
      />
    </Page>
  );
}

function CategoryDialog({ category, onClose }: { category?: Category; onClose: () => void }) {
  const [name, setName] = useState(category?.name ?? "");
  const [savePath, setSavePath] = useState(category?.savePath ?? "");
  const { data: settings } = useQuery(settingsQuery);
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT("/categories/{name}", {
          params: { path: { name: name.trim() } },
          body: { savePath: savePath.trim() },
        }),
      ),
    onSuccess: (c) => {
      qc.setQueryData<Category[]>(keys.categories, (prev) =>
        [...(prev ?? []).filter((x) => x.name !== c.name), c].toSorted((a, b) =>
          a.name.localeCompare(b.name),
        ),
      );
      toast.success(category ? `Saved ${c.name}` : `Created ${c.name}`);
      onClose();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) {
      save.mutate();
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>{category ? "Edit category" : "New category"}</DialogTitle>
            <DialogDescription>
              Torrents in the category are saved under its path.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="category-name">Name</FieldLabel>
              <Input
                id="category-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={category !== undefined}
                autoFocus={!category}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="category-path">Save path</FieldLabel>
              <Input
                id="category-path"
                value={savePath}
                onChange={(e) => setSavePath(e.target.value)}
                placeholder={settings?.settings.defaultSavePath}
                className="font-mono text-xs"
                autoFocus={category !== undefined}
              />
              <FieldDescription>Empty uses the default save path.</FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" size="sm" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={!name.trim() || save.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DeleteDialog({
  target,
  count,
  onClose,
}: {
  target: { kind: "category" | "tag"; name: string } | null;
  count: number;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: async (t: { kind: "category" | "tag"; name: string }) => {
      if (t.kind === "category") {
        await unwrap(api.DELETE("/categories/{name}", { params: { path: { name: t.name } } }));
      } else {
        await unwrap(api.DELETE("/tags/{name}", { params: { path: { name: t.name } } }));
      }
      return t;
    },
    onSuccess: (t) => {
      if (t.kind === "category") {
        qc.setQueryData<Category[]>(keys.categories, (prev) =>
          prev?.filter((c) => c.name !== t.name),
        );
      } else {
        qc.setQueryData<string[]>(keys.tags, (prev) => prev?.filter((x) => x !== t.name));
      }
      onClose();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <AlertDialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            Delete {target?.kind} “{target?.name}”?
          </AlertDialogTitle>
          <AlertDialogDescription>
            {count > 0
              ? `${plural(count, "torrent")} ${count === 1 ? "loses" : "lose"} it. Their files stay where they are.`
              : `No torrent uses this ${target?.kind}.`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel size="sm">Cancel</AlertDialogCancel>
          <AlertDialogAction
            size="sm"
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => target && remove.mutate(target)}
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
