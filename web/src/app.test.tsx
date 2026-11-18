import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { Principal } from "@/api/types";
import { FakeEventSource } from "@/test/fake-event-source";
import { torrent, transfer } from "@/test/fixtures";
import { App } from "./app";

const admin: Principal = {
  userId: 1,
  username: "admin",
  name: "admin",
  isAdmin: true,
  method: "session",
  scope: "full",
};

interface Call {
  method: string;
  path: string;
  body: unknown;
}

/** Answers API calls from a table of "METHOD /path" handlers and records them. */
function mockApi(routes: Record<string, (body: unknown) => Response | Promise<Response>>) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const req =
        input instanceof Request
          ? input
          : new Request(new URL(String(input), location.origin), init);
      const url = new URL(req.url);
      const path = url.pathname.replace("/api/piecemeal/v1", "");
      const text = req.method === "GET" ? "" : await req.text();
      const body = text ? JSON.parse(text) : undefined;
      calls.push({ method: req.method, path, body });
      const handler = routes[`${req.method} ${path}`];
      return handler ? handler(body) : json({ title: "Not Found" }, 404);
    }),
  );
  return calls;
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

beforeEach(() => {
  FakeEventSource.reset();
  vi.stubGlobal("EventSource", FakeEventSource);
  history.replaceState(null, "", "/");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const ubuntu = torrent({
  hash: "1".repeat(40),
  name: "Ubuntu ISO",
  state: "downloading",
  downloadRate: 2048,
});
const music = torrent({
  hash: "2".repeat(40),
  name: "Music",
  state: "seeding",
  complete: true,
  progress: 1,
  category: "music",
});

function snapshot() {
  act(() => {
    FakeEventSource.latest().emit("snapshot", {
      torrents: [ubuntu, music],
      transfer: transfer({ downloadRate: 2048 }),
      categories: [{ name: "music", savePath: "" }],
      tags: ["favorite"],
    });
  });
}

test("signing in with a password leads to the live torrent list", async () => {
  let signedIn = false;
  const calls = mockApi({
    "GET /auth/me": () => (signedIn ? json(admin) : json({ title: "Unauthorized" }, 401)),
    "GET /auth/config": () => json({ localLogin: true, oidc: true, oidcName: "Pocket ID" }),
    "POST /auth/login": (body) => {
      const { password } = body as { password: string };
      if (password !== "secret") {
        return json({ title: "Unauthorized", detail: "wrong" }, 401);
      }
      signedIn = true;
      return json(admin);
    },
    "GET /settings": () =>
      json({ settings: { addStopped: false, altSpeedEnabled: false }, locked: {} }),
    "GET /categories": () => json({ items: [] }),
    "GET /tags": () => json({ items: [] }),
  });
  const user = userEvent.setup();
  render(<App />);

  expect(await screen.findByRole("heading", { name: "piecemeal" })).toBeTruthy();
  expect(
    (await screen.findByRole("link", { name: /sign in with pocket id/i })).getAttribute("href"),
  ).toBe("/api/piecemeal/v1/auth/oidc/login?next=%2F");

  await user.type(screen.getByLabelText("Password"), "nope");
  await user.click(screen.getByRole("button", { name: /^sign in$/i }));
  expect(await screen.findByText("Wrong username or password.")).toBeTruthy();

  await user.clear(screen.getByLabelText("Password"));
  await user.type(screen.getByLabelText("Password"), "secret");
  await user.click(screen.getByRole("button", { name: /^sign in$/i }));
  expect(calls.filter((c) => c.path === "/auth/login").at(-1)?.body).toEqual({
    username: "admin",
    password: "secret",
  });

  expect(await screen.findByText("Loading torrents…")).toBeTruthy();
  snapshot();
  const grid = await screen.findByRole("grid", { name: "Torrents" });
  expect(within(grid).getByText("Ubuntu ISO")).toBeTruthy();
  expect(within(grid).getByText("Music")).toBeTruthy();
  expect(within(grid).getByText("2.00 KiB/s")).toBeTruthy();
});

test("filters, selection and removing torrents", async () => {
  history.replaceState(null, "", "/?filter=downloading");
  const calls = mockApi({
    "GET /auth/me": () => json(admin),
    "GET /settings": () =>
      json({ settings: { addStopped: false, altSpeedEnabled: false }, locked: {} }),
    "GET /categories": () => json({ items: [] }),
    "GET /tags": () => json({ items: [] }),
    "POST /torrents/actions": () => new Response(null, { status: 204 }),
  });
  const user = userEvent.setup();
  render(<App />);
  await screen.findByText("Loading torrents…");
  snapshot();

  // The URL's filter applies, and the sidebar counts every state.
  const grid = await screen.findByRole("grid", { name: "Torrents" });
  expect(within(grid).queryByText("Music")).toBeNull();
  expect(within(grid).getByText("Ubuntu ISO")).toBeTruthy();
  const seedingLink = screen.getByRole("link", { name: /^Seeding/ });
  expect(seedingLink.parentElement?.textContent).toContain("1");

  const [allStates] = screen.getAllByRole("link", { name: /^All/ });
  await user.click(allStates as HTMLElement);
  await vi.waitFor(() => expect(within(grid).getByText("Music")).toBeTruthy());

  // Select both rows with their checkboxes, then remove them with the keyboard.
  await user.click(screen.getByRole("checkbox", { name: "Select Ubuntu ISO" }));
  await user.click(screen.getByRole("checkbox", { name: "Select Music" }));
  expect(screen.getByText("2 selected")).toBeTruthy();
  await user.keyboard("{Delete}");
  const dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText("Remove 2 torrents?")).toBeTruthy();
  await user.click(within(dialog).getByRole("checkbox"));
  await user.click(within(dialog).getByRole("button", { name: "Remove and delete files" }));

  await vi.waitFor(() =>
    expect(calls.find((c) => c.path === "/torrents/actions")?.body).toEqual({
      action: "remove",
      // In list order: newest first, ties by infohash.
      hashes: [ubuntu.hash, music.hash],
      deleteData: true,
    }),
  );
  // Removed torrents leave the list without waiting for the stream.
  await vi.waitFor(() => expect(screen.getByText("No torrents yet")).toBeTruthy());
});

test("the sign-in page shows why an SSO sign-in failed", async () => {
  mockApi({
    "GET /auth/me": () => json({ title: "Unauthorized" }, 401),
    "GET /auth/config": () => json({ localLogin: true, oidc: true }),
  });
  history.pushState({}, "", "/login?error=Your+account+is+not+allowed");
  render(<App />);
  expect((await screen.findByRole("alert")).textContent).toBe("Your account is not allowed");
  expect(screen.getByRole("link", { name: /sign in with sso/i })).toBeTruthy();
});

test("a 401 from the API sends the user to the sign-in page", async () => {
  // The session is valid when the app starts, then ends: the transfer bar's
  // settings request answers 401.
  mockApi({
    "GET /auth/me": () => json(admin),
    "GET /auth/config": () => json({ localLogin: true, oidc: false }),
    "GET /settings": () => json({ title: "Unauthorized" }, 401),
    "GET /categories": () => json({ items: [] }),
    "GET /tags": () => json({ items: [] }),
  });
  render(<App />);
  expect(await screen.findByLabelText("Password")).toBeTruthy();
  expect(location.pathname).toBe("/login");
});

test("anyone signed in can switch the alternative speed limits, unless they are pinned", async () => {
  const user = { ...admin, userId: 2, username: "viewer", name: "Viewer", isAdmin: false };
  let locked: Record<string, string> = {};
  const calls = mockApi({
    "GET /auth/me": () => json(user),
    "GET /settings": () => json({ settings: { altSpeedEnabled: false }, locked }),
    "GET /categories": () => json({ items: [] }),
    "GET /tags": () => json({ items: [] }),
    "PATCH /transfer": (body) => json(transfer({ ...(body as object) })),
  });
  const u = userEvent.setup();
  const { unmount } = render(<App />);
  await screen.findByText("Loading torrents…");
  snapshot();
  const toggle = await screen.findByRole("button", { name: "Alternative speed limits" });
  expect(toggle.getAttribute("aria-pressed")).toBe("false");
  await u.click(toggle);
  await vi.waitFor(() => expect(toggle.getAttribute("aria-pressed")).toBe("true"));
  expect(calls.filter((c) => c.method === "PATCH")).toEqual([
    { method: "PATCH", path: "/transfer", body: { altSpeedEnabled: true } },
  ]);
  unmount();

  // Pinned by an environment variable: shown, but not switchable.
  locked = { altSpeedEnabled: "PIECEMEAL_ALT_SPEED_ENABLED" };
  FakeEventSource.reset();
  render(<App />);
  await screen.findByText("Loading torrents…");
  snapshot();
  const pinned = await screen.findByRole("button", { name: "Alternative speed limits" });
  await vi.waitFor(() => expect(pinned.hasAttribute("disabled")).toBe(true));
});
