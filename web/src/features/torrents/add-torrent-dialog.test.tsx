import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { keys } from "@/lib/queries";
import { torrent } from "@/test/fixtures";
import { extractMagnets } from "./add-store";
import { AddTorrentDialog, fileToBase64, lines } from "./add-torrent-dialog";

afterEach(() => {
  vi.unstubAllGlobals();
});

function setup(request = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(keys.settings, {
    settings: { defaultSavePath: "/downloads", addStopped: false },
    locked: {},
  });
  qc.setQueryData(keys.categories, [{ name: "tv", savePath: "/media/tv" }]);
  qc.setQueryData(keys.tags, ["favorite"]);
  const onClose = vi.fn<() => void>();
  render(
    <QueryClientProvider client={qc}>
      <AddTorrentDialog request={request} onClose={onClose} />
    </QueryClientProvider>,
  );
  return { qc, onClose };
}

test("adds each magnet link with the chosen options", async () => {
  const bodies: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const body = (await req.json()) as { magnet: string };
      bodies.push(body);
      if (body.magnet.includes("dupe")) {
        return new Response(JSON.stringify({ title: "Conflict", status: 409 }), { status: 409 });
      }
      return new Response(JSON.stringify(torrent({ hash: String(bodies.length).repeat(40) })), {
        status: 201,
      });
    }),
  );
  const user = userEvent.setup();
  const { onClose } = setup({ magnets: "magnet:?xt=urn:btih:one\n\nmagnet:?xt=urn:btih:dupe" });
  const dialog = await screen.findByRole("dialog");
  // The save path hint follows the default, and the button counts the links.
  expect(within(dialog).getByLabelText("Save path").getAttribute("placeholder")).toBe("/downloads");
  await user.click(within(dialog).getByRole("button", { name: "favorite" }));
  await user.click(within(dialog).getByRole("checkbox", { name: "Add stopped" }));
  await user.click(within(dialog).getByRole("button", { name: "Add 2 torrents" }));

  expect(await within(dialog).findByText("Already added")).toBeTruthy();
  expect(bodies).toEqual([
    { magnet: "magnet:?xt=urn:btih:one", tags: ["favorite"], stopped: true },
    { magnet: "magnet:?xt=urn:btih:dupe", tags: ["favorite"], stopped: true },
  ]);
  // Only the failed link is left to retry, and the dialog stays open.
  expect((within(dialog).getByLabelText(/magnet links/i) as HTMLTextAreaElement).value).toBe(
    "magnet:?xt=urn:btih:dupe",
  );
  expect(onClose).not.toHaveBeenCalled();
});

test("helpers", async () => {
  expect(lines(" a \r\n\nb\n")).toEqual(["a", "b"]);
  expect(await fileToBase64(new Blob(["hello"]))).toBe(btoa("hello"));
  expect(
    extractMagnets(
      `see magnet:?xt=urn:btih:abc and ${"A".repeat(40)} or ${"b".repeat(32)} nothing`,
    ),
  ).toEqual([
    "magnet:?xt=urn:btih:abc",
    `magnet:?xt=urn:btih:${"A".repeat(40)}`,
    `magnet:?xt=urn:btih:${"b".repeat(32)}`,
  ]);
});
