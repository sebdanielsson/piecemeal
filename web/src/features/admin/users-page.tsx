import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { User } from "@/api/types";
import { DataTable, type DataColumn } from "@/components/data/data-table";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { unwrap } from "@/lib/api";
import { formatDateTime, formatRelative } from "@/lib/format";
import { keys } from "@/lib/queries";
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

export function UsersPage() {
  const list = useQuery({
    queryKey: keys.users,
    queryFn: async () => (await unwrap(api.GET("/users"))).items ?? [],
  });
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
          columns={columns}
          rowKey={(u) => String(u.id)}
          initialSort={{ id: "username", desc: false }}
          label="Users"
        />
      )}
    </Page>
  );
}
