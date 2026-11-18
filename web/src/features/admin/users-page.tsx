import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2Icon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { User } from "@/api/types";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { errorMessage, unwrap } from "@/lib/api";
import { formatDateTime, formatRelative } from "@/lib/format";
import { keys, meQuery } from "@/lib/queries";
import { Page } from "../shell/page";

const columns: DataColumn<User>[] = [
  {
    id: "username",
    header: "User",
    cell: (u) => (
      <span className="flex flex-col">
        <span className="font-medium">{u.displayName || u.username}</span>
        {u.displayName && u.displayName !== u.username && (
          <span className="text-xs text-muted-foreground">{u.username}</span>
        )}
      </span>
    ),
    sort: (u) => u.username,
  },
  { id: "email", header: "Email", cell: (u) => u.email || "–", sort: (u) => u.email },
  {
    id: "role",
    header: "Role",
    cell: (u) => (
      <Badge variant={u.isAdmin ? "default" : "secondary"}>{u.isAdmin ? "Admin" : "User"}</Badge>
    ),
    sort: (u) => (u.isAdmin ? 0 : 1),
  },
  {
    id: "signin",
    header: "Signs in with",
    cell: (u) => (u.oidc ? "SSO" : "Password"),
    sort: (u) => (u.oidc ? 0 : 1),
  },
  {
    id: "last",
    header: "Last sign-in",
    cell: (u) =>
      u.lastLoginAt ? (
        <span title={formatDateTime(u.lastLoginAt)}>{formatRelative(u.lastLoginAt)}</span>
      ) : (
        "Never"
      ),
    sort: (u) => u.lastLoginAt ?? "",
  },
  {
    id: "created",
    header: "Created",
    cell: (u) => formatDateTime(u.createdAt),
    sort: (u) => u.createdAt,
  },
];

// The local admin comes from the configuration, and admins can't remove
// themselves.
function removable(u: User, myID: number | undefined) {
  return u.oidc && u.id !== myID;
}

function makeUserColumns(
  myID: number | undefined,
  onRemove: (u: User) => void,
): DataColumn<User>[] {
  return [
    ...columns,
    {
      id: "actions",
      header: "",
      className: "w-24 text-right",
      cell: (u) =>
        removable(u, myID) && (
          <Button size="xs" variant="ghost" onClick={() => onRemove(u)}>
            <Trash2Icon data-icon="inline-start" />
            Remove
          </Button>
        ),
    },
  ];
}

export function UsersPage() {
  const qc = useQueryClient();
  const { data: me } = useQuery(meQuery);
  const list = useQuery({
    queryKey: keys.users,
    queryFn: async () => (await unwrap(api.GET("/users"))).items ?? [],
  });
  const [removing, setRemoving] = useState<User | null>(null);
  const remove = useMutation({
    mutationFn: (u: User) => unwrap(api.DELETE("/users/{id}", { params: { path: { id: u.id } } })),
    onSuccess: () => {
      setRemoving(null);
      void qc.invalidateQueries({ queryKey: keys.users });
      void qc.invalidateQueries({ queryKey: keys.sessions });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const myID = me?.userId;
  const withActions = useMemo(() => makeUserColumns(myID, setRemoving), [myID]);
  return (
    <Page
      title="Users"
      description="People who have signed in. SSO users are created on their first sign-in."
    >
      {list.isPending ? (
        <div className="flex justify-center p-6">
          <Spinner />
        </div>
      ) : list.isError ? (
        <p className="text-destructive">Couldn't load the users: {list.error.message}</p>
      ) : (
        <DataTable
          rows={list.data}
          columns={withActions}
          rowKey={(u) => String(u.id)}
          initialSort={{ id: "username", desc: false }}
          label="Users"
        />
      )}
      <AlertDialog open={removing !== null} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Remove {removing?.displayName || removing?.username}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              Their sessions end and their API keys stop working. If they're still allowed to sign
              in at your identity provider, they can sign in again as a new user, so remove them
              from the allowed groups there first.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel size="sm">Cancel</AlertDialogCancel>
            <AlertDialogAction
              size="sm"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => removing && remove.mutate(removing)}
            >
              Remove user
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Page>
  );
}
