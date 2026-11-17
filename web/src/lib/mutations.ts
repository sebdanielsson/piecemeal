import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/api/client";
import type {
  BulkActionName,
  FilePriority,
  Settings,
  SettingsResponse,
  TorrentFile,
  UpdateTorrentRequest,
} from "@/api/types";
import { errorMessage, unwrap } from "./api";
import { plural } from "./format";
import { removeTorrents, upsertTorrent, type TorrentMap } from "./live/reducer";
import { keys } from "./queries";

export interface TorrentActionInput {
  action: BulkActionName;
  hashes: string[];
  category?: string;
  tags?: string[];
  deleteData?: boolean;
}

const actionVerbs: Record<BulkActionName, string> = {
  start: "Started",
  stop: "Stopped",
  forceStart: "Force-started",
  recheck: "Rechecking",
  reannounce: "Reannounced",
  remove: "Removed",
  setCategory: "Set the category of",
  addTags: "Tagged",
  removeTags: "Untagged",
};

/**
 * Runs an action on one or more torrents through the bulk endpoint. The
 * event stream delivers the resulting changes; removals are applied at once.
 */
export function useTorrentAction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: TorrentActionInput) => {
      await unwrap(
        api.POST("/torrents/actions", {
          body: {
            action: input.action,
            hashes: input.hashes,
            category: input.category,
            tags: input.tags,
            deleteData: input.deleteData,
          },
        }),
      );
      return input;
    },
    onSuccess: (input) => {
      if (input.action === "remove") {
        qc.setQueryData<TorrentMap>(keys.torrents, (prev) => removeTorrents(prev, input.hashes));
      }
      if (input.action === "remove" || input.hashes.length > 1) {
        toast.success(`${actionVerbs[input.action]} ${plural(input.hashes.length, "torrent")}`);
      }
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
}

/** Changes one torrent's properties and applies the result at once. */
export function useUpdateTorrent(hash: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: UpdateTorrentRequest) =>
      unwrap(api.PATCH("/torrents/{hash}", { params: { path: { hash } }, body })),
    onSuccess: (t) => qc.setQueryData<TorrentMap>(keys.torrents, (prev) => upsertTorrent(prev, t)),
    onError: (error) => toast.error(errorMessage(error)),
  });
}

/** Sets file priorities, updating the cached file list optimistically. */
export function useSetFilePriority(hash: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ indices, priority }: { indices: number[]; priority: FilePriority }) => {
      await unwrap(
        api.PATCH("/torrents/{hash}/files", {
          params: { path: { hash } },
          body: { indices, priority },
        }),
      );
    },
    onMutate: async ({ indices, priority }) => {
      await qc.cancelQueries({ queryKey: keys.files(hash) });
      const prev = qc.getQueryData<TorrentFile[]>(keys.files(hash));
      const set = new Set(indices);
      qc.setQueryData<TorrentFile[]>(keys.files(hash), (files) =>
        files?.map((f) => (set.has(f.index) ? { ...f, priority } : f)),
      );
      return { prev };
    },
    onError: (error, _vars, ctx) => {
      qc.setQueryData(keys.files(hash), ctx?.prev);
      toast.error(errorMessage(error));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: keys.files(hash) }),
  });
}

/** Changes settings (admins only); the response is the new settings. */
export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: Partial<Settings>) => unwrap(api.PATCH("/settings", { body: patch })),
    onSuccess: (data) => qc.setQueryData<SettingsResponse>(keys.settings, data),
    onError: (error) => toast.error(errorMessage(error)),
  });
}
