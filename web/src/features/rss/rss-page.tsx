import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircleIcon,
  CheckCheckIcon,
  DownloadIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  RssIcon,
  Trash2Icon,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { RSSArticle, RSSFeed, RSSRule } from "@/api/types";
import { DataTable, type DataColumn } from "@/components/data/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "cn";
import { errorMessage, unwrap } from "@/lib/api";
import { formatDateTime, formatRelative } from "@/lib/format";
import { htmlToText } from "@/lib/html";
import { keys, meQuery, rssQuery, rssRulesQuery } from "@/lib/queries";
import { Page } from "../shell/page";
import { FeedDialog } from "./feed-dialog";
import { RuleDialog } from "./rule-dialog";

export function RSSPage() {
  const { data: me } = useQuery(meQuery);
  const admin = me?.isAdmin === true;
  const [tab, setTab] = useState<"feeds" | "rules">("feeds");
  return (
    <Page
      title="RSS"
      description="Feeds are fetched on their schedule, and download rules add the torrents of new articles they match."
    >
      <Tabs value={tab} onValueChange={(v) => setTab(v as "feeds" | "rules")}>
        <TabsList>
          <TabsTrigger value="feeds">Feeds</TabsTrigger>
          <TabsTrigger value="rules">Download rules</TabsTrigger>
        </TabsList>
        <TabsContent value="feeds" className="pt-4">
          <Feeds admin={admin} />
        </TabsContent>
        <TabsContent value="rules" className="pt-4">
          <Rules admin={admin} />
        </TabsContent>
      </Tabs>
    </Page>
  );
}

function Feeds({ admin }: { admin: boolean }) {
  const qc = useQueryClient();
  const { data, isPending, isError, error } = useQuery(rssQuery);
  const [selected, setSelected] = useState<string>();
  const [editing, setEditing] = useState<RSSFeed | "new">();
  const refresh = useMutation({
    mutationFn: (path: string) => unwrap(api.POST("/rss/refresh", { params: { query: { path } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.rss }),
    onError: (e) => toast.error(errorMessage(e)),
  });
  if (isPending) {
    return <Spinner />;
  }
  if (isError) {
    return <p className="text-destructive">Couldn't load the feeds: {error.message}</p>;
  }
  const feeds = data.feeds ?? [];
  const current = feeds.find((f) => f.uid === selected) ?? feeds[0];
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        {admin && (
          <Button size="sm" onClick={() => setEditing("new")}>
            <PlusIcon data-icon="inline-start" />
            Add feed
          </Button>
        )}
        <Button
          size="sm"
          variant="outline"
          disabled={feeds.length === 0 || refresh.isPending}
          onClick={() => refresh.mutate("")}
        >
          <RefreshCwIcon data-icon="inline-start" />
          Refresh all
        </Button>
      </div>
      {feeds.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <RssIcon className="size-6 text-muted-foreground" />
            <EmptyTitle>No feeds</EmptyTitle>
            <EmptyDescription>
              Add a tracker's RSS feed, then a download rule that picks the releases you want.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="grid gap-4 md:grid-cols-[16rem_1fr]">
          <ul className="flex flex-col border" aria-label="Feeds">
            {feeds.map((f) => (
              <li key={f.uid}>
                <button
                  type="button"
                  onClick={() => setSelected(f.uid)}
                  aria-current={f.uid === current?.uid || undefined}
                  className={cn(
                    "flex w-full items-center gap-2 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/50",
                    f.uid === current?.uid && "bg-muted",
                  )}
                >
                  {f.loading ? (
                    <Spinner className="size-3.5" />
                  ) : f.error ? (
                    <AlertCircleIcon
                      className="size-3.5 text-destructive"
                      aria-label="Fetch failed"
                    />
                  ) : (
                    <RssIcon className="size-3.5 text-muted-foreground" />
                  )}
                  <span className="min-w-0 flex-1 truncate" title={f.path}>
                    {f.path.replaceAll("\\", " / ")}
                  </span>
                  {f.unread > 0 && <Badge variant="secondary">{f.unread}</Badge>}
                </button>
              </li>
            ))}
          </ul>
          {current && (
            <FeedArticles
              feed={current}
              admin={admin}
              onEdit={() => setEditing(current)}
              onRefresh={() => refresh.mutate(current.path)}
            />
          )}
        </div>
      )}
      <FeedDialog
        feed={editing === "new" ? undefined : editing}
        open={editing !== undefined}
        onOpenChange={(open) => !open && setEditing(undefined)}
      />
    </div>
  );
}

function FeedArticles({
  feed,
  admin,
  onEdit,
  onRefresh,
}: {
  feed: RSSFeed;
  admin: boolean;
  onEdit: () => void;
  onRefresh: () => void;
}) {
  const qc = useQueryClient();
  const { data, isPending } = useQuery({
    queryKey: keys.rssFeed(feed.uid),
    queryFn: () => unwrap(api.GET("/rss/feeds/{uid}", { params: { path: { uid: feed.uid } } })),
    refetchInterval: feed.loading ? 2_000 : 30_000,
  });
  const invalidate = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: keys.rss }),
      qc.invalidateQueries({ queryKey: keys.rssFeed(feed.uid) }),
    ]);
  const markRead = useMutation({
    mutationFn: (articleId?: string) =>
      unwrap(api.POST("/rss/read", { body: { path: feed.path, articleId } })),
    onSuccess: invalidate,
    onError: (e) => toast.error(errorMessage(e)),
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/rss/items", { params: { query: { path: feed.path } } })),
    onSuccess: () => {
      toast.success(`Removed ${feed.path}`);
      return qc.invalidateQueries({ queryKey: keys.rss });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const add = useMutation({
    mutationFn: async (a: RSSArticle) => {
      const url = a.torrentURL ?? "";
      await unwrap(
        api.POST("/torrents", { body: url.startsWith("magnet:") ? { magnet: url } : { url } }),
      );
      return a;
    },
    onSuccess: (a) => {
      toast.success(`Added ${a.title}`);
      markRead.mutate(a.id);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const articles = data?.articles ?? [];
  return (
    <section className="flex min-w-0 flex-col gap-3" aria-label={feed.path}>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-medium">{feed.title || feed.path}</h2>
          <p className="truncate font-mono text-xs text-muted-foreground" title={feed.url}>
            {feed.url}
          </p>
        </div>
        <Button size="sm" variant="ghost" onClick={onRefresh}>
          <RefreshCwIcon data-icon="inline-start" />
          Refresh
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={feed.unread === 0}
          onClick={() => markRead.mutate(undefined)}
        >
          <CheckCheckIcon data-icon="inline-start" />
          Mark all read
        </Button>
        {admin && (
          <>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Edit feed"
              title="Edit feed"
              onClick={onEdit}
            >
              <PencilIcon />
            </Button>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Remove feed"
              title="Remove feed"
              onClick={() => remove.mutate()}
            >
              <Trash2Icon />
            </Button>
          </>
        )}
      </div>
      {feed.error && (
        <p
          role="alert"
          className="border border-destructive/40 bg-destructive/5 p-2 text-sm text-destructive"
        >
          The last fetch failed: {feed.error}
        </p>
      )}
      {isPending ? (
        <Spinner />
      ) : articles.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {feed.fetchedAt ? "No articles." : "Not fetched yet."}
        </p>
      ) : (
        <ul className="flex flex-col border" aria-label="Articles">
          {articles.map((a) => (
            <li key={a.id} className="flex items-start gap-3 border-b px-3 py-2 last:border-b-0">
              <div className="min-w-0 flex-1">
                <p
                  className={cn(
                    "truncate text-sm",
                    a.read ? "text-muted-foreground" : "font-medium",
                  )}
                  title={a.title}
                >
                  {a.title}
                </p>
                <p className="truncate text-xs text-muted-foreground">
                  <span title={formatDateTime(a.date)}>{formatRelative(a.date)}</span>
                  {a.category && <> · {a.category}</>}
                  {a.description && <> · {htmlToText(a.description)}</>}
                </p>
              </div>
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={`Download ${a.title}`}
                title="Download"
                disabled={!a.torrentURL || add.isPending}
                onClick={() => add.mutate(a)}
              >
                <DownloadIcon />
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

type RuleRow = { name: string; rule: RSSRule };

function makeRuleColumns(
  admin: boolean,
  onEdit: (r: RuleRow) => void,
  onRemove: (name: string) => void,
): DataColumn<RuleRow>[] {
  const columns: DataColumn<RuleRow>[] = [
    {
      id: "name",
      header: "Name",
      cell: (r) =>
        admin ? (
          <button type="button" className="font-medium hover:underline" onClick={() => onEdit(r)}>
            {r.name}
          </button>
        ) : (
          <span className="font-medium">{r.name}</span>
        ),
      sort: (r) => r.name,
    },
    {
      id: "enabled",
      header: "State",
      cell: (r) =>
        r.rule.enabled ? (
          <Badge variant="secondary">On</Badge>
        ) : (
          <Badge variant="outline">Off</Badge>
        ),
      sort: (r) => (r.rule.enabled ? 0 : 1),
      className: "w-20",
    },
    {
      id: "contains",
      header: "Must contain",
      cell: (r) => <span className="font-mono text-xs">{r.rule.mustContain || "anything"}</span>,
      sort: (r) => r.rule.mustContain,
    },
    {
      id: "feeds",
      header: "Feeds",
      cell: (r) => (r.rule.affectedFeeds ?? []).length,
      sort: (r) => (r.rule.affectedFeeds ?? []).length,
      className: "w-20",
    },
    {
      id: "last",
      header: "Last match",
      cell: (r) => (r.rule.lastMatch ? formatRelative(r.rule.lastMatch) : "never"),
      sort: (r) => r.rule.lastMatch ?? "",
    },
  ];
  if (admin) {
    columns.push({
      id: "actions",
      header: "",
      cell: (r) => (
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={`Remove ${r.name}`}
          title="Remove rule"
          onClick={() => onRemove(r.name)}
        >
          <Trash2Icon />
        </Button>
      ),
      className: "w-12",
    });
  }
  return columns;
}

function Rules({ admin }: { admin: boolean }) {
  const qc = useQueryClient();
  const { data: rules = {}, isPending } = useQuery(rssRulesQuery);
  const [editing, setEditing] = useState<{ name: string; rule?: RSSRule }>();
  const remove = useMutation({
    mutationFn: (name: string) =>
      unwrap(api.DELETE("/rss/rules/{name}", { params: { path: { name } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.rssRules }),
    onError: (e) => toast.error(errorMessage(e)),
  });
  const rows = Object.entries(rules).map(([name, rule]) => ({ name, rule }));
  const columns = makeRuleColumns(admin, setEditing, (name) => remove.mutate(name));
  if (isPending) {
    return <Spinner />;
  }
  return (
    <div className="flex flex-col gap-4">
      {admin && (
        <div>
          <Button size="sm" onClick={() => setEditing({ name: "" })}>
            <PlusIcon data-icon="inline-start" />
            New rule
          </Button>
        </div>
      )}
      <DataTable
        rows={rows}
        columns={columns}
        rowKey={(r) => r.name}
        initialSort={{ id: "name", desc: false }}
        empty="No download rules."
        label="Download rules"
      />
      <RuleDialog name={editing?.name} rule={editing?.rule} onClose={() => setEditing(undefined)} />
    </div>
  );
}
