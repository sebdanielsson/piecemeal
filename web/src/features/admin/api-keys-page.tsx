import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CopyIcon, PlusIcon, Trash2Icon, TriangleAlertIcon } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { APIKey, APIKeyScope, CreatedAPIKey } from "@/api/types";
import { DataTable, type DataColumn } from "@/components/data/data-table";
import { OptionSelect } from "@/components/form/option-select";
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
import { Spinner } from "@/components/ui/spinner";
import { errorMessage, unwrap } from "@/lib/api";
import { copyText } from "@/lib/clipboard";
import { formatDateTime, formatRelative } from "@/lib/format";
import { keys, meQuery } from "@/lib/queries";
import { Page } from "../shell/page";

export const scopeOptions: { value: APIKeyScope; label: string; description: string }[] = [
  {
    value: "full",
    label: "Full access",
    description: "Everything the owner can do, in both APIs.",
  },
  {
    value: "qbittorrent",
    label: "qBittorrent API",
    description: "Only the qBittorrent-compatible API, for Radarr, Sonarr and Prowlarr.",
  },
  { value: "read-only", label: "Read-only", description: "Can look but not change anything." },
];

const scopeLabel = (s: APIKeyScope) => scopeOptions.find((o) => o.value === s)?.label ?? s;

function makeKeyColumns(isAdmin: boolean, onDelete: (k: APIKey) => void): DataColumn<APIKey>[] {
  return [
    {
      id: "name",
      header: "Name",
      cell: (k) => <span className="font-medium">{k.name}</span>,
      sort: (k) => k.name,
    },
    {
      id: "prefix",
      header: "Key",
      cell: (k) => <code className="font-mono text-xs">{k.prefix}…</code>,
    },
    { id: "scope", header: "Scope", cell: (k) => scopeLabel(k.scope), sort: (k) => k.scope },
    ...(isAdmin
      ? [
          {
            id: "user",
            header: "User",
            cell: (k: APIKey) => k.username,
            sort: (k: APIKey) => k.username,
          },
        ]
      : []),
    {
      id: "created",
      header: "Created",
      cell: (k) => <span title={formatDateTime(k.createdAt)}>{formatRelative(k.createdAt)}</span>,
      sort: (k) => k.createdAt,
    },
    {
      id: "used",
      header: "Last used",
      cell: (k) =>
        k.lastUsedAt ? (
          <span title={formatDateTime(k.lastUsedAt)}>{formatRelative(k.lastUsedAt)}</span>
        ) : (
          "Never"
        ),
      sort: (k) => k.lastUsedAt ?? "",
    },
    {
      id: "actions",
      header: "",
      className: "w-12 text-right",
      cell: (k) => (
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label={`Delete ${k.name}`}
          onClick={() => onDelete(k)}
        >
          <Trash2Icon />
        </Button>
      ),
    },
  ];
}

export function APIKeysPage() {
  const qc = useQueryClient();
  const { data: me } = useQuery(meQuery);
  const list = useQuery({
    queryKey: keys.apiKeys,
    queryFn: async () => (await unwrap(api.GET("/api-keys"))).items ?? [],
  });
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedAPIKey | null>(null);
  const [deleting, setDeleting] = useState<APIKey | null>(null);

  const remove = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/api-keys/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      void qc.invalidateQueries({ queryKey: keys.apiKeys });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const columns = useMemo(() => makeKeyColumns(me?.isAdmin === true, setDeleting), [me?.isAdmin]);

  return (
    <Page
      title="API keys"
      description="For tools such as Radarr, Sonarr and scripts. Send a key as Authorization: Bearer <key>."
      actions={
        <Button size="sm" onClick={() => setCreating(true)} disabled={me?.method !== "session"}>
          <PlusIcon data-icon="inline-start" />
          New key
        </Button>
      }
    >
      {list.isPending ? (
        <div className="flex justify-center p-6">
          <Spinner />
        </div>
      ) : list.isError ? (
        <p className="text-destructive">Couldn't load the keys: {list.error.message}</p>
      ) : (
        <DataTable
          rows={list.data}
          columns={columns}
          rowKey={(k) => String(k.id)}
          initialSort={{ id: "created", desc: true }}
          empty="No API keys yet."
          label="API keys"
        />
      )}
      {creating && (
        <CreateKeyDialog
          onClose={() => setCreating(false)}
          onCreated={(k) => {
            setCreating(false);
            setCreated(k);
            void qc.invalidateQueries({ queryKey: keys.apiKeys });
          }}
        />
      )}
      <SecretDialog created={created} onClose={() => setCreated(null)} />
      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete “{deleting?.name}”?</AlertDialogTitle>
            <AlertDialogDescription>
              Anything using this key loses access straight away. This can't be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel size="sm">Cancel</AlertDialogCancel>
            <AlertDialogAction
              size="sm"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => deleting && remove.mutate(deleting.id)}
            >
              Delete key
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Page>
  );
}

function CreateKeyDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (k: CreatedAPIKey) => void;
}) {
  const [name, setName] = useState("");
  const [scope, setScope] = useState<APIKeyScope>("qbittorrent");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/api-keys", { body: { name: name.trim(), scope } })),
    onSuccess: onCreated,
    onError: (e) => toast.error(errorMessage(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) {
      create.mutate();
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>New API key</DialogTitle>
            <DialogDescription>The key is shown once, right after it's created.</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="key-name">Name</FieldLabel>
              <Input
                id="key-name"
                value={name}
                maxLength={100}
                onChange={(e) => setName(e.target.value)}
                placeholder="Radarr"
                autoFocus
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="key-scope">Scope</FieldLabel>
              <OptionSelect
                id="key-scope"
                value={scope}
                onChange={setScope}
                options={scopeOptions}
              />
              <FieldDescription>
                {scopeOptions.find((o) => o.value === scope)?.description}
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" size="sm" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={!name.trim() || create.isPending}>
              Create key
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function SecretDialog({
  created,
  onClose,
}: {
  created: CreatedAPIKey | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={created !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Copy the key now</DialogTitle>
          <DialogDescription className="flex items-start gap-2">
            <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-state-warning" />
            piecemeal stores only a hash of “{created?.name}”. Once you close this, the key can't be
            shown again.
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 border bg-muted/50 p-2">
          <code
            className="min-w-0 flex-1 font-mono text-xs break-all select-all"
            aria-label="API key"
          >
            {created?.key}
          </code>
          <Button
            size="sm"
            variant="outline"
            onClick={() => created && void copyText(created.key, "Copied the API key")}
          >
            <CopyIcon data-icon="inline-start" />
            Copy
          </Button>
        </div>
        <DialogFooter>
          <Button size="sm" onClick={onClose}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
