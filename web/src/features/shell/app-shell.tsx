import { useQueryClient } from "@tanstack/react-query";
import { Outlet, useNavigate, useRouter } from "@tanstack/react-router";
import { UploadIcon } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { setUnauthorizedHandler } from "@/lib/api";
import { useHotkeys } from "@/lib/hotkeys";
import { connectLiveStream } from "@/lib/live/stream";
import { forgetSession, keys } from "@/lib/queries";
import { useStore } from "@/lib/store";
import { addDialog, extractMagnets, isTorrentFile, openAddDialog } from "../torrents/add-store";
import { AppSidebar } from "./app-sidebar";
import { ShortcutsDialog } from "./shortcuts-dialog";
import { TransferBar } from "./transfer-bar";

const AddTorrentDialog = lazy(() =>
  import("../torrents/add-torrent-dialog").then((m) => ({ default: m.AddTorrentDialog })),
);

export function AppShell() {
  const queryClient = useQueryClient();
  const router = useRouter();
  const navigate = useNavigate();
  const [helpOpen, setHelpOpen] = useState(false);
  const addRequest = useStore(addDialog);

  // The session can end under us (expiry, signed out elsewhere): go to the
  // sign-in page and come back here afterwards.
  useEffect(() => {
    const onUnauthorized = () => {
      queryClient.setQueryData(keys.me, null);
      void navigate({
        to: "/login",
        search: { redirect: router.state.location.href },
        replace: true,
      }).then(() => forgetSession(queryClient));
    };
    setUnauthorizedHandler(onUnauthorized);
    const disconnect = connectLiveStream(queryClient, { onUnauthorized });
    return () => {
      disconnect();
      setUnauthorizedHandler(undefined);
    };
  }, [queryClient, navigate, router]);

  useHotkeys({
    a: () => openAddDialog(),
    "?": () => setHelpOpen(true),
  });

  usePasteToAdd();
  const dragging = useDropToAdd();

  return (
    <SidebarProvider className="h-svh min-h-0">
      <AppSidebar onShowShortcuts={() => setHelpOpen(true)} />
      <SidebarInset className="min-h-0 min-w-0 overflow-hidden">
        <div className="flex min-h-0 flex-1 flex-col">
          <Outlet />
        </div>
        <TransferBar />
      </SidebarInset>
      {addRequest && (
        <Suspense fallback={null}>
          <AddTorrentDialog request={addRequest} onClose={() => addDialog.set(null)} />
        </Suspense>
      )}
      <ShortcutsDialog open={helpOpen} onOpenChange={setHelpOpen} />
      {dragging && (
        <div className="pointer-events-none fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
          <div className="flex flex-col items-center gap-3 border-2 border-dashed border-primary px-12 py-10">
            <UploadIcon className="size-8" />
            <p className="font-heading text-2xl">Drop .torrent files to add them</p>
          </div>
        </div>
      )}
    </SidebarProvider>
  );
}

/** Pasting magnet links or .torrent files anywhere outside a text field adds them. */
function usePasteToAdd() {
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (
        target?.closest("input, textarea, [contenteditable=true]") ||
        addDialog.get() !== null ||
        !e.clipboardData
      ) {
        return;
      }
      const files = [...e.clipboardData.files].filter(isTorrentFile);
      if (files.length > 0) {
        e.preventDefault();
        openAddDialog({ tab: "file", files });
        return;
      }
      const magnets = extractMagnets(e.clipboardData.getData("text"));
      if (magnets.length > 0) {
        e.preventDefault();
        openAddDialog({ tab: "magnet", magnets: magnets.join("\n") });
      }
    };
    document.addEventListener("paste", onPaste);
    return () => document.removeEventListener("paste", onPaste);
  }, []);
}

/** Dropping .torrent files anywhere opens the add dialog with them. */
function useDropToAdd(): boolean {
  const [dragging, setDragging] = useState(false);
  useEffect(() => {
    let depth = 0;
    const hasFiles = (e: DragEvent) => e.dataTransfer?.types.includes("Files") ?? false;
    const onEnter = (e: DragEvent) => {
      if (!hasFiles(e) || addDialog.get() !== null) {
        return;
      }
      depth++;
      setDragging(true);
    };
    const onLeave = () => {
      depth = Math.max(0, depth - 1);
      if (depth === 0) {
        setDragging(false);
      }
    };
    const onOver = (e: DragEvent) => {
      if (hasFiles(e) && addDialog.get() === null) {
        e.preventDefault();
      }
    };
    const onDrop = (e: DragEvent) => {
      depth = 0;
      setDragging(false);
      if (addDialog.get() !== null || !e.dataTransfer) {
        return;
      }
      const files = [...e.dataTransfer.files].filter(isTorrentFile);
      if (files.length > 0 || hasFiles(e)) {
        e.preventDefault();
      }
      if (files.length > 0) {
        openAddDialog({ tab: "file", files });
      }
    };
    window.addEventListener("dragenter", onEnter);
    window.addEventListener("dragleave", onLeave);
    window.addEventListener("dragover", onOver);
    window.addEventListener("drop", onDrop);
    return () => {
      window.removeEventListener("dragenter", onEnter);
      window.removeEventListener("dragleave", onLeave);
      window.removeEventListener("dragover", onOver);
      window.removeEventListener("drop", onDrop);
    };
  }, []);
  return dragging;
}
