import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { Category, Settings } from "@/api/types";
import { keys } from "@/lib/queries";
import { torrent } from "@/test/fixtures";
import { extractMagnets } from "./add-store";
import { AddTorrentDialog, defaultPaths, fileToBase64, lines } from "./add-torrent-dialog";

afterEach(() => {
  vi.unstubAllGlobals();
});

function setup(request = {}, settings = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(keys.settings, {
    settings: { defaultSavePath: "/downloads", addStopped: false, ...settings },
    locked: {},
  });
  qc.setQueryData(keys.categories, [
    { name: "tv", savePath: "/media/tv", incompletePathMode: "default", incompletePath: "" },
    { name: "films", savePath: "", incompletePathMode: "off", incompletePath: "" },
  ]);
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

function captureBodies(): unknown[] {
  const bodies: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      bodies.push(await req.json());
      return new Response(JSON.stringify(torrent()), { status: 201 });
    }),
  );
  return bodies;
}

test("automatic management takes the category's paths", async () => {
  const bodies = captureBodies();
  const user = userEvent.setup();
  const { onClose } = setup(
    { magnets: "magnet:?xt=urn:btih:one" },
    { incompletePathEnabled: true, incompletePath: "/downloads/temp" },
  );
  const dialog = await screen.findByRole("dialog");
  // Manual: the global incomplete folder is on by default.
  const keep = within(dialog).getByRole("checkbox", { name: /incomplete folder/i });
  expect(keep.getAttribute("aria-checked")).toBe("true");
  expect(within(dialog).getByLabelText("Incomplete folder").getAttribute("placeholder")).toBe(
    "/downloads/temp",
  );
  await user.click(within(dialog).getByRole("checkbox", { name: /automatic management/i }));
  // Automatic: the paths come from the category, here none.
  const path = within(dialog).getByLabelText("Save path");
  expect(path.hasAttribute("disabled")).toBe(true);
  expect(path.getAttribute("placeholder")).toBe("/downloads");
  await user.click(within(dialog).getByRole("button", { name: "Add torrent" }));
  await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
  expect(bodies).toEqual([
    { magnet: "magnet:?xt=urn:btih:one", autoManaged: true, stopped: false },
  ]);
});

test("the incomplete folder can be turned off or changed", async () => {
  const bodies = captureBodies();
  const user = userEvent.setup();
  const { onClose } = setup(
    { magnets: "magnet:?xt=urn:btih:one\nmagnet:?xt=urn:btih:two" },
    { incompletePathEnabled: true, incompletePath: "/downloads/temp" },
  );
  const dialog = await screen.findByRole("dialog");
  await user.type(within(dialog).getByLabelText("Incomplete folder"), "/downloads/elsewhere");
  await user.click(within(dialog).getByRole("button", { name: "Add 2 torrents" }));
  await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
  expect(bodies).toEqual([
    { magnet: "magnet:?xt=urn:btih:one", incompletePath: "/downloads/elsewhere", stopped: false },
    { magnet: "magnet:?xt=urn:btih:two", incompletePath: "/downloads/elsewhere", stopped: false },
  ]);
});

test("turning the incomplete folder off asks for none", async () => {
  const bodies = captureBodies();
  const user = userEvent.setup();
  const { onClose } = setup(
    { magnets: "magnet:?xt=urn:btih:one" },
    { incompletePathEnabled: true, incompletePath: "/downloads/temp" },
  );
  const dialog = await screen.findByRole("dialog");
  await user.click(within(dialog).getByRole("checkbox", { name: /incomplete folder/i }));
  expect(within(dialog).queryByLabelText("Incomplete folder")).toBeNull();
  await user.click(within(dialog).getByRole("button", { name: "Add torrent" }));
  await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
  expect(bodies).toEqual([
    { magnet: "magnet:?xt=urn:btih:one", incompletePath: "", stopped: false },
  ]);
});

const pathSettings = {
  defaultSavePath: "/downloads",
  incompletePathEnabled: true,
  incompletePath: "/downloads/temp",
} as Settings;

function category(over: Partial<Category> = {}): Category {
  return {
    name: "tv",
    savePath: "",
    incompletePathMode: "default",
    incompletePath: "",
    ratioLimit: -2,
    seedingTimeLimit: -2,
    inactiveSeedingTimeLimit: -2,
    seedGoalAction: "default",
    seedGoalMode: "default",
    ...over,
  };
}

test("defaultPaths follows the category, then the settings", () => {
  expect(defaultPaths(pathSettings, undefined, false)).toEqual({
    savePath: "/downloads",
    incompletePath: "/downloads/temp",
  });
  expect(defaultPaths(pathSettings, category(), true)).toEqual({
    savePath: "/downloads/tv",
    incompletePath: "/downloads/temp/tv",
  });
  expect(defaultPaths(pathSettings, category(), false)).toEqual({
    savePath: "/downloads",
    incompletePath: "/downloads/temp",
  });
  expect(
    defaultPaths(pathSettings, category({ savePath: "shows", incompletePathMode: "off" }), true),
  ).toEqual({ savePath: "/downloads/shows", incompletePath: "" });
  expect(
    defaultPaths(
      { ...pathSettings, incompletePathEnabled: false },
      category({ incompletePathMode: "on", incompletePath: "/inc" }),
      false,
    ),
  ).toEqual({ savePath: "/downloads", incompletePath: "/inc" });
});
