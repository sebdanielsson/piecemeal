import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { torrent } from "@/test/fixtures";
import { RemoveDialog } from "./remove-dialog";

afterEach(() => {
  vi.unstubAllGlobals();
});

// Like the torrents page: the dialog stays mounted and the page opens it.
function Page() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open
      </button>
      <RemoveDialog torrents={[torrent({ name: "Ubuntu" })]} open={open} onOpenChange={setOpen} />
    </>
  );
}

test("deleting files is never ticked when the dialog opens again", async () => {
  const bodies: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      bodies.push(await req.json());
      return new Response(null, { status: 204 });
    }),
  );
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Page />
    </QueryClientProvider>,
  );

  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await screen.findByRole("checkbox", { name: /delete the downloaded files/i }));
  await user.click(screen.getByRole("button", { name: "Remove and delete files" }));
  await vi.waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(bodies).toEqual([{ action: "remove", hashes: [torrent().hash], deleteData: true }]);

  await user.click(screen.getByRole("button", { name: "Open" }));
  const box = await screen.findByRole("checkbox", { name: /delete the downloaded files/i });
  expect(box.getAttribute("aria-checked")).toBe("false");
  expect(screen.getByRole("button", { name: "Remove" })).toBeTruthy();
});
