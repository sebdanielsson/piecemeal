import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { keys } from "@/lib/queries";
import { torrent } from "@/test/fixtures";
import { GeneralTab } from "./general-tab";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("the goals form says what the defaults come to and saves the action and mode", async () => {
  const requests: { method: string; body: unknown }[] = [];
  const t = torrent({
    category: "tv",
    ratioLimit: -2,
    seedingTimeLimit: 90,
    seedGoals: {
      ratioLimit: 2,
      seedingTimeLimit: 90,
      inactiveSeedingTimeLimit: -1,
      action: "remove",
      mode: "all",
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const body = await req.json();
      requests.push({ method: req.method, body });
      return new Response(JSON.stringify({ ...t, ...(body as object) }), { status: 200 });
    }),
  );
  const qc = new QueryClient();
  qc.setQueryData(keys.categories, [{ name: "tv", savePath: "" }]);
  qc.setQueryData(keys.tags, []);
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={qc}>
      <GeneralTab torrent={t} />
    </QueryClientProvider>,
  );

  expect(screen.getByLabelText("Ratio goal").textContent).toContain("Default (2.00)");
  // The torrent's own goal: the default's value isn't known.
  expect(screen.getByLabelText("Seeding time goal").textContent).toContain("Minutes");
  expect(screen.getByLabelText("Inactive time goal").textContent).toContain("Default (Unlimited)");
  expect(screen.getByLabelText("Reached when").textContent).toContain(
    "Default (every goal set is met)",
  );
  expect(screen.getByLabelText("Then").textContent).toContain("Default (remove the torrent)");

  await user.click(screen.getByLabelText("Then"));
  await user.click(await screen.findByRole("option", { name: "Stop the torrent" }));
  await user.click(screen.getByLabelText("Reached when"));
  await user.click(await screen.findByRole("option", { name: "Any goal is met" }));
  await user.click(screen.getByRole("button", { name: "Save goals" }));
  await vi.waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]).toEqual({
    method: "PATCH",
    body: { seedGoalAction: "stop", seedGoalMode: "any" },
  });
});
