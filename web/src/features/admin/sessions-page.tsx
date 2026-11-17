import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { LogOutIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { Session } from "@/api/types";
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

/** A short description of a browser from its user agent. */
export function describeUserAgent(ua: string): string {
  if (!ua) {
    return "Unknown client";
  }
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /Firefox\//.test(ua)
      ? "Firefox"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Safari\//.test(ua)
          ? "Safari"
          : undefined;
  const os = /Windows/.test(ua)
    ? "Windows"
    : /iPhone|iPad/.test(ua)
      ? "iOS"
      : /Mac OS X|Macintosh/.test(ua)
        ? "macOS"
        : /Android/.test(ua)
          ? "Android"
          : /Linux/.test(ua)
            ? "Linux"
            : undefined;
  if (browser && os) {
    return `${browser} on ${os}`;
  }
  return ua.length > 60 ? `${ua.slice(0, 57)}…` : ua;
}

function makeSessionColumns(isAdmin: boolean, onEnd: (s: Session) => void): DataColumn<Session>[] {
  return [
    {
      id: "client",
      header: "Client",
      cell: (s) => (
        <span className="flex items-center gap-2">
          <span title={s.userAgent}>{describeUserAgent(s.userAgent)}</span>
          {s.current && <Badge variant="secondary">This browser</Badge>}
          {s.kind === "qbittorrent" && <Badge variant="outline">qBittorrent API</Badge>}
        </span>
      ),
      sort: (s) => s.userAgent,
    },
    {
      id: "ip",
      header: "Address",
      cell: (s) => <span className="font-mono text-xs">{s.ip}</span>,
      sort: (s) => s.ip,
    },
    ...(isAdmin
      ? [
          {
            id: "user",
            header: "User",
            cell: (s: Session) => s.userId,
            sort: (s: Session) => s.userId,
          },
        ]
      : []),
    {
      id: "seen",
      header: "Last seen",
      cell: (s) => <span title={formatDateTime(s.lastSeenAt)}>{formatRelative(s.lastSeenAt)}</span>,
      sort: (s) => s.lastSeenAt,
    },
    {
      id: "created",
      header: "Signed in",
      cell: (s) => <span title={formatDateTime(s.createdAt)}>{formatRelative(s.createdAt)}</span>,
      sort: (s) => s.createdAt,
    },
    {
      id: "expires",
      header: "Expires",
      cell: (s) => formatDateTime(s.expiresAt),
      sort: (s) => s.expiresAt,
    },
    {
      id: "actions",
      header: "",
      className: "w-24 text-right",
      cell: (s) => (
        <Button size="xs" variant="ghost" onClick={() => onEnd(s)}>
          <LogOutIcon data-icon="inline-start" />
          End
        </Button>
      ),
    },
  ];
}

export function SessionsPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: me } = useQuery(meQuery);
  const list = useQuery({
    queryKey: keys.sessions,
    queryFn: async () => (await unwrap(api.GET("/sessions"))).items ?? [],
    refetchInterval: 30_000,
  });
  const [ending, setEnding] = useState<Session | null>(null);
  const end = useMutation({
    mutationFn: (s: Session) =>
      unwrap(api.DELETE("/sessions/{id}", { params: { path: { id: s.id } } })),
    onSuccess: async (_d, s) => {
      setEnding(null);
      if (s.current) {
        await navigate({ to: "/login", search: {} });
        qc.clear();
        return;
      }
      void qc.invalidateQueries({ queryKey: keys.sessions });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const columns = useMemo(() => makeSessionColumns(me?.isAdmin === true, setEnding), [me?.isAdmin]);

  return (
    <Page
      title="Sessions"
      description="Where you're signed in. Ending a session signs that browser or client out."
    >
      {list.isPending ? (
        <div className="flex justify-center p-6">
          <Spinner />
        </div>
      ) : list.isError ? (
        <p className="text-destructive">Couldn't load the sessions: {list.error.message}</p>
      ) : (
        <DataTable
          rows={list.data}
          columns={columns}
          rowKey={(s) => s.id}
          initialSort={{ id: "seen", desc: true }}
          label="Sessions"
        />
      )}
      <AlertDialog open={ending !== null} onOpenChange={(open) => !open && setEnding(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {ending?.current ? "Sign out of this browser?" : "End this session?"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {ending?.current
                ? "This is the session you're using now."
                : `${describeUserAgent(ending?.userAgent ?? "")} at ${ending?.ip} will have to sign in again.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel size="sm">Cancel</AlertDialogCancel>
            <AlertDialogAction
              size="sm"
              variant="destructive"
              disabled={end.isPending}
              onClick={() => ending && end.mutate(ending)}
            >
              {ending?.current ? "Sign out" : "End session"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Page>
  );
}
