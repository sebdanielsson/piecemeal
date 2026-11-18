import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/api/client";
import type {
  BulkActionName,
  FilePriority,
  Settings,
  SettingsResponse,
  TorrentFile,
  TransferLimits,
  TransferStatus,
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
  savePath?: string;
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
  move: "Moving",
  queueTop: "Moved to the top of the queue:",
  queueBottom: "Moved to the bottom of the queue:",
  queueUp: "Moved up the queue:",
  queueDown: "Moved down the queue:",
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
            savePath: input.savePath,
          },
        }),
      );
      return input;
    },
    onSuccess: (input) => {
      if (input.action === "remove") {
        qc.setQueryData<TorrentMap>(keys.torrents, (prev) => removeTorrents(prev, input.hashes));
      }
      if (input.action === "remove" || input.action === "move" || input.hashes.length > 1) {
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

/** Replaces a torrent's trackers and web seeds. */
export function useSetTrackers(hash: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ tiers, webSeeds }: { tiers: string[][]; webSeeds: string[] }) => {
      const path = { hash };
      await unwrap(api.PUT("/torrents/{hash}/trackers", { params: { path }, body: { tiers } }));
      await unwrap(
        api.PUT("/torrents/{hash}/web-seeds", { params: { path }, body: { urls: webSeeds } }),
      );
    },
    onSuccess: () => toast.success("Saved the trackers"),
    onError: (error) => toast.error(errorMessage(error)),
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: keys.trackers(hash) }),
        qc.invalidateQueries({ queryKey: keys.webSeeds(hash) }),
      ]),
  });
}

/** Renames a file or folder of a torrent, on disk too. */
export function useRenamePath(hash: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: { oldPath: string; newPath: string }) => {
      await unwrap(api.POST("/torrents/{hash}/files/rename", { params: { path: { hash } }, body }));
    },
    onError: (error) => toast.error(errorMessage(error)),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.files(hash) }),
  });
}

/**
 * Changes the global and alternative speed limits and the alternative speed
 * toggle. Unlike settings, any signed-in user may. The settings event
 * follows; the cached settings and transfer status update at once.
 */
export function useUpdateTransfer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: TransferLimits) => unwrap(api.PATCH("/transfer", { body: patch })),
    onSuccess: (status, patch) => {
      qc.setQueryData<TransferStatus>(keys.transfer, status);
      qc.setQueryData<SettingsResponse>(
        keys.settings,
        (prev) => prev && { ...prev, settings: { ...prev.settings, ...patch } },
      );
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
}

/** The settings PATCH /transfer changes, which every signed-in user may. */
export const transferSettingKeys = new Set<keyof Settings>([
  "altSpeedEnabled",
  "downloadLimit",
  "uploadLimit",
  "altDownloadLimit",
  "altUploadLimit",
]);

/** Changes settings (admins only); the response is the new settings. */
export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: Partial<Settings>) => unwrap(api.PATCH("/settings", { body: patch })),
    onSuccess: (data) => qc.setQueryData<SettingsResponse>(keys.settings, data),
    onError: (error) => toast.error(errorMessage(error)),
  });
}
