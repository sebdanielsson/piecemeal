import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileIcon, LinkIcon, MagnetIcon, UploadIcon, XIcon } from "lucide-react";
import { useMemo, useRef, useState, type DragEvent, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { AddTorrentRequest } from "@/api/types";
import { CategorySelect } from "@/components/form/category-select";
import { TagPicker } from "@/components/form/tag-picker";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { ApiError, errorMessage, unwrap } from "@/lib/api";
import { formatBytes, plural } from "@/lib/format";
import { upsertTorrent, type TorrentMap } from "@/lib/live/reducer";
import { categoriesQuery, keys, settingsQuery } from "@/lib/queries";
import { isTorrentFile, type AddRequest } from "./add-store";

type Tab = "magnet" | "file" | "url";

/** Reads a file as base64, for AddTorrentRequest.torrentFile. */
export function fileToBase64(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.addEventListener("error", () =>
      reject(reader.error ?? new Error("couldn't read the file")),
    );
    reader.addEventListener("load", () => {
      const url = String(reader.result);
      resolve(url.slice(url.indexOf(",") + 1));
    });
    reader.readAsDataURL(file);
  });
}

/** Non-empty lines. */
export function lines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean);
}

interface Failure {
  source: string;
  message: string;
}

export function AddTorrentDialog({
  request,
  onClose,
}: {
  request: AddRequest;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const { data: settings } = useQuery(settingsQuery);
  const { data: categories = [] } = useQuery(categoriesQuery);
  const [tab, setTab] = useState<Tab>(request.tab ?? (request.files?.length ? "file" : "magnet"));
  const [magnets, setMagnets] = useState(request.magnets ?? "");
  const [urls, setUrls] = useState("");
  const [files, setFiles] = useState<File[]>(request.files ?? []);
  const [category, setCategory] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [savePath, setSavePath] = useState("");
  const [stopped, setStopped] = useState<boolean | undefined>(undefined);
  const [sequential, setSequential] = useState(false);
  const [firstLastPiece, setFirstLastPiece] = useState(false);
  const [skipChecking, setSkipChecking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failures, setFailures] = useState<Failure[]>([]);
  const fileInput = useRef<HTMLInputElement>(null);
  const [dragOver, setDragOver] = useState(false);

  const startStopped = stopped ?? settings?.settings.addStopped ?? false;
  const categoryPath = categories.find((c) => c.name === category)?.savePath;
  const defaultPath = categoryPath || settings?.settings.defaultSavePath || "";

  const sources = useMemo(() => {
    if (tab === "magnet") {
      return lines(magnets).map((m) => ({ label: m, body: { magnet: m } as AddTorrentRequest }));
    }
    if (tab === "url") {
      return lines(urls).map((u) => ({ label: u, body: { url: u } as AddTorrentRequest }));
    }
    return files.map((f) => ({ label: f.name, file: f }));
  }, [tab, magnets, urls, files]);

  const addFiles = (list: Iterable<File>) => {
    const next = [...list].filter(isTorrentFile);
    if (next.length === 0) {
      toast.error("Only .torrent files can be added");
      return;
    }
    setFiles((prev) => {
      const seen = new Set(prev.map((f) => `${f.name}:${f.size}`));
      return [...prev, ...next.filter((f) => !seen.has(`${f.name}:${f.size}`))];
    });
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragOver(false);
    addFiles(e.dataTransfer.files);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (sources.length === 0) {
      return;
    }
    setBusy(true);
    setFailures([]);
    const options: AddTorrentRequest = {
      category: category || undefined,
      tags: tags.length > 0 ? tags : undefined,
      savePath: savePath.trim() || undefined,
      stopped: startStopped,
      sequential: sequential || undefined,
      firstLastPiece: firstLastPiece || undefined,
      skipChecking: skipChecking || undefined,
    };
    const failed: Failure[] = [];
    let added = 0;
    for (const source of sources) {
      try {
        const body: AddTorrentRequest =
          "file" in source
            ? { ...options, torrentFile: await fileToBase64(source.file) }
            : { ...options, ...source.body };
        const t = await unwrap(api.POST("/torrents", { body }));
        queryClient.setQueryData<TorrentMap>(keys.torrents, (prev) => upsertTorrent(prev, t));
        added++;
      } catch (error) {
        const message =
          error instanceof ApiError && error.status === 409 ? "Already added" : errorMessage(error);
        failed.push({ source: source.label, message });
      }
    }
    setBusy(false);
    if (added > 0) {
      toast.success(`Added ${plural(added, "torrent")}`);
    }
    if (failed.length === 0) {
      onClose();
    } else {
      setFailures(failed);
      // Keep only what failed, so trying again doesn't add duplicates.
      const failedSet = new Set(failed.map((f) => f.source));
      if (tab === "magnet") {
        setMagnets(
          lines(magnets)
            .filter((m) => failedSet.has(m))
            .join("\n"),
        );
      } else if (tab === "url") {
        setUrls(
          lines(urls)
            .filter((u) => failedSet.has(u))
            .join("\n"),
        );
      } else {
        setFiles((prev) => prev.filter((f) => failedSet.has(f.name)));
      }
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-xl">
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Add torrents</DialogTitle>
            <DialogDescription>
              Magnet links, .torrent files or links to .torrent files.
            </DialogDescription>
          </DialogHeader>
          <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
            <TabsList className="w-full">
              <TabsTrigger value="magnet">
                <MagnetIcon />
                Magnet
              </TabsTrigger>
              <TabsTrigger value="file">
                <FileIcon />
                File
              </TabsTrigger>
              <TabsTrigger value="url">
                <LinkIcon />
                URL
              </TabsTrigger>
            </TabsList>
            <TabsContent value="magnet" className="pt-4">
              <Field>
                <FieldLabel htmlFor="add-magnets">Magnet links, one per line</FieldLabel>
                <Textarea
                  id="add-magnets"
                  value={magnets}
                  onChange={(e) => setMagnets(e.target.value)}
                  rows={4}
                  placeholder="magnet:?xt=urn:btih:…"
                  className="font-mono text-xs"
                  autoFocus
                />
              </Field>
            </TabsContent>
            <TabsContent value="file" className="pt-4">
              <div
                onDragOver={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setDragOver(true);
                }}
                onDragLeave={() => setDragOver(false)}
                onDrop={onDrop}
                className={
                  "flex flex-col items-center gap-2 border border-dashed px-4 py-6 text-center transition-colors " +
                  (dragOver ? "border-primary bg-accent" : "border-border")
                }
              >
                <UploadIcon className="size-6 text-muted-foreground" />
                <p className="text-sm">Drop .torrent files here</p>
                <Button
                  type="button"
                  size="xs"
                  variant="outline"
                  onClick={() => fileInput.current?.click()}
                >
                  Choose files
                </Button>
                <input
                  ref={fileInput}
                  type="file"
                  accept=".torrent,application/x-bittorrent"
                  multiple
                  className="sr-only"
                  aria-label="Choose .torrent files"
                  onChange={(e) => {
                    if (e.target.files) {
                      addFiles(e.target.files);
                    }
                    e.target.value = "";
                  }}
                />
              </div>
              {files.length > 0 && (
                <ul className="mt-3 flex max-h-40 flex-col overflow-y-auto border-y">
                  {files.map((f) => (
                    <li
                      key={`${f.name}:${f.size}`}
                      className="flex items-center gap-2 border-b py-1.5 text-sm last:border-b-0"
                    >
                      <FileIcon className="size-3.5 shrink-0 text-muted-foreground" />
                      <span className="min-w-0 flex-1 truncate">{f.name}</span>
                      <span className="text-xs text-muted-foreground tabular">
                        {formatBytes(f.size)}
                      </span>
                      <button
                        type="button"
                        className="p-1 text-muted-foreground hover:text-foreground"
                        aria-label={`Remove ${f.name}`}
                        onClick={() => setFiles((prev) => prev.filter((x) => x !== f))}
                      >
                        <XIcon className="size-3.5" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </TabsContent>
            <TabsContent value="url" className="pt-4">
              <Field>
                <FieldLabel htmlFor="add-urls">URLs, one per line</FieldLabel>
                <Textarea
                  id="add-urls"
                  value={urls}
                  onChange={(e) => setUrls(e.target.value)}
                  rows={3}
                  placeholder="https://example.org/file.torrent"
                  className="font-mono text-xs"
                />
              </Field>
            </TabsContent>
          </Tabs>

          <FieldGroup className="gap-5">
            <div className="grid gap-5 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="add-category">Category</FieldLabel>
                <CategorySelect id="add-category" value={category} onChange={setCategory} />
              </Field>
              <Field>
                <FieldLabel htmlFor="add-path">Save path</FieldLabel>
                <Input
                  id="add-path"
                  value={savePath}
                  onChange={(e) => setSavePath(e.target.value)}
                  placeholder={defaultPath}
                  className="font-mono text-xs"
                />
              </Field>
            </div>
            <Field>
              <FieldLabel htmlFor="add-tags">Tags</FieldLabel>
              <TagPicker id="add-tags" label="Tags" value={tags} onChange={setTags} />
            </Field>
            <div className="grid gap-3 sm:grid-cols-2">
              <CheckOption checked={startStopped} onChange={setStopped} label="Add stopped" />
              <CheckOption
                checked={sequential}
                onChange={setSequential}
                label="Download in order"
              />
              <CheckOption
                checked={firstLastPiece}
                onChange={setFirstLastPiece}
                label="First and last pieces first"
              />
              <CheckOption
                checked={skipChecking}
                onChange={setSkipChecking}
                label="Skip hash check"
              />
            </div>
          </FieldGroup>

          {failures.length > 0 && (
            <div role="alert" className="border border-destructive/40 bg-destructive/5 p-3 text-sm">
              <p className="mb-1 font-medium text-destructive">
                {plural(failures.length, "torrent")} couldn't be added:
              </p>
              <ul className="flex flex-col gap-1">
                {failures.map((f) => (
                  <li key={f.source} className="min-w-0">
                    <span className="block truncate font-mono text-xs">{f.source}</span>
                    <span className="text-muted-foreground">{f.message}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <DialogFooter>
            <Button type="button" size="sm" variant="outline" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={busy || sources.length === 0}>
              {busy && <Spinner />}
              {sources.length > 1 ? `Add ${sources.length} torrents` : "Add torrent"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function CheckOption({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
}) {
  return (
    <Label className="flex items-center gap-2.5 font-normal normal-case">
      <Checkbox checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      {label}
    </Label>
  );
}
