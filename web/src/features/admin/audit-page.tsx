import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { unwrap } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { keys } from "@/lib/queries";
import { Page } from "../shell/page";

const pageSize = 100;

export function AuditPage() {
  const audit = useInfiniteQuery({
    queryKey: keys.audit,
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      (
        await unwrap(
          api.GET("/audit", {
            params: { query: { limit: pageSize, before: pageParam || undefined } },
          }),
        )
      ).items ?? [],
    getNextPageParam: (last) => (last.length < pageSize ? undefined : last.at(-1)?.id),
  });
  const entries = audit.data?.pages.flat() ?? [];
  return (
    <Page
      title="Audit log"
      description="Sign-ins, API key changes and setting changes, newest first."
    >
      {audit.isPending ? (
        <div className="flex justify-center p-6">
          <Spinner />
        </div>
      ) : audit.isError ? (
        <p className="text-destructive">Couldn't load the audit log: {audit.error.message}</p>
      ) : (
        <>
          <Table aria-label="Audit log">
            <TableHeader>
              <TableRow>
                <TableHead className="h-8 text-xs">When</TableHead>
                <TableHead className="h-8 text-xs">Who</TableHead>
                <TableHead className="h-8 text-xs">What</TableHead>
                <TableHead className="h-8 text-xs">Detail</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="py-6 text-center text-muted-foreground">
                    Nothing logged yet.
                  </TableCell>
                </TableRow>
              )}
              {entries.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className="py-1.5 text-muted-foreground tabular">
                    {formatDateTime(e.at)}
                  </TableCell>
                  <TableCell className="py-1.5">{e.actor}</TableCell>
                  <TableCell className="py-1.5 font-mono text-xs">{e.action}</TableCell>
                  <TableCell className="py-1.5 whitespace-normal">{e.detail}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {audit.hasNextPage && (
            <Button
              size="sm"
              variant="outline"
              className="self-center"
              onClick={() => void audit.fetchNextPage()}
              disabled={audit.isFetchingNextPage}
            >
              {audit.isFetchingNextPage && <Spinner />}
              Load older entries
            </Button>
          )}
        </>
      )}
    </Page>
  );
}
