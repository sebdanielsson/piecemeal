import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { Category } from "@/api/types";
import { keys } from "@/lib/queries";
import { CategoryDialog, describeGoals } from "./categories-page";

afterEach(() => {
  vi.unstubAllGlobals();
});

const tv: Category = {
  name: "tv",
  savePath: "/media/tv",
  ratioLimit: 1.5,
  seedingTimeLimit: -2,
  inactiveSeedingTimeLimit: -1,
  seedGoalAction: "default",
  seedGoalMode: "all",
};

test("describeGoals lists what the category sets", () => {
  expect(
    describeGoals({
      ...tv,
      ratioLimit: -2,
      inactiveSeedingTimeLimit: -2,
      seedGoalMode: "default",
    }),
  ).toBe("");
  expect(describeGoals({ ...tv, seedGoalAction: "removeWithData" })).toBe(
    "Ratio 1.50, inactive unlimited, every goal set is met, remove the torrent and its files",
  );
});

test("the category editor saves the seeding goals with the save path", async () => {
  const requests: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const body = await req.json();
      requests.push({ method: req.method, path: new URL(req.url).pathname, body });
      return new Response(JSON.stringify({ ...tv, ...(body as object) }), { status: 200 });
    }),
  );
  const qc = new QueryClient();
  qc.setQueryData(keys.settings, { settings: { defaultSavePath: "/downloads" }, locked: {} });
  const onClose = vi.fn<() => void>();
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={qc}>
      <CategoryDialog category={tv} onClose={onClose} />
    </QueryClientProvider>,
  );

  expect(screen.getByLabelText("Ratio goal").textContent).toContain("Ratio");
  expect(screen.getByLabelText("Seeding time goal").textContent).toContain("Global setting");
  expect(screen.getByLabelText("Reached when").textContent).toContain("Every goal set is met");
  await user.click(screen.getByLabelText("Then"));
  await user.click(await screen.findByRole("option", { name: "Remove the torrent" }));
  await user.click(screen.getByRole("button", { name: "Save" }));

  await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
  expect(requests).toEqual([
    {
      method: "PUT",
      path: "/api/piecemeal/v1/categories/tv",
      body: {
        savePath: "/media/tv",
        ratioLimit: 1.5,
        seedingTimeLimit: -2,
        inactiveSeedingTimeLimit: -1,
        seedGoalAction: "remove",
        seedGoalMode: "all",
      },
    },
  ]);
});
