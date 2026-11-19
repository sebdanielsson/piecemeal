import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { torrent } from "@/test/fixtures";
import { MoveDialog } from "./move-dialog";

afterEach(() => {
  vi.unstubAllGlobals();
});

function setup(torrents = [torrent({ incompletePath: "/downloads/temp", autoManaged: true })]) {
  const bodies: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      bodies.push(await req.json());
      return new Response(null, { status: 204 });
    }),
  );
  const onOpenChange = vi.fn<(open: boolean) => void>();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MoveDialog torrents={torrents} open onOpenChange={onOpenChange} />
    </QueryClientProvider>,
  );
  return { bodies, onOpenChange };
}

test("changes only the incomplete folder", async () => {
  const { bodies, onOpenChange } = setup();
  const user = userEvent.setup();
  expect(screen.getByText(/Automatic management turns off/)).toBeTruthy();
  const button = screen.getByRole("button", { name: "Move" });
  expect(button.hasAttribute("disabled")).toBe(true);
  const incomplete = screen.getByLabelText("Incomplete folder");
  await user.clear(incomplete);
  await user.click(button);
  await vi.waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  expect(bodies).toEqual([{ action: "move", hashes: ["a".repeat(40)], incompletePath: "" }]);
});

test("moves the data and changes the incomplete folder", async () => {
  const { bodies, onOpenChange } = setup([torrent()]);
  const user = userEvent.setup();
  expect(screen.queryByText(/Automatic management turns off/)).toBeNull();
  await user.clear(screen.getByLabelText("Save path"));
  await user.type(screen.getByLabelText("Save path"), "/downloads/new");
  await user.type(screen.getByLabelText("Incomplete folder"), "/downloads/inc");
  await user.click(screen.getByRole("button", { name: "Move" }));
  await vi.waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  expect(bodies).toEqual([
    {
      action: "move",
      hashes: ["a".repeat(40)],
      savePath: "/downloads/new",
      incompletePath: "/downloads/inc",
    },
  ]);
});

test("removes differing incomplete folders", async () => {
  const { bodies, onOpenChange } = setup([
    torrent({ incompletePath: "/downloads/a" }),
    torrent({ hash: "b".repeat(40), incompletePath: "/downloads/b" }),
  ]);
  const user = userEvent.setup();
  const incomplete = screen.getByLabelText("Incomplete folder");
  expect((incomplete as HTMLInputElement).value).toBe("");
  await user.type(incomplete, "x");
  await user.clear(incomplete);
  const button = screen.getByRole("button", { name: "Move" });
  expect(button.hasAttribute("disabled")).toBe(false);
  await user.click(button);
  await vi.waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  expect(bodies).toEqual([
    { action: "move", hashes: ["a".repeat(40), "b".repeat(40)], incompletePath: "" },
  ]);
});
