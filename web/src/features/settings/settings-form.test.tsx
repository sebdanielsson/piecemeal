import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { Principal, Settings, SettingsResponse } from "@/api/types";
import { keys } from "@/lib/queries";
import { transfer } from "@/test/fixtures";
import { altSpeedGroup, proxyGroup, speedGroup } from "./groups";
import { SettingsForm } from "./settings-form";

afterEach(() => {
  vi.unstubAllGlobals();
});

const settings = {
  downloadLimit: 0,
  uploadLimit: 0,
  altSpeedEnabled: false,
  altDownloadLimit: 0,
  altUploadLimit: 0,
} as Settings;

test("unsaved edits sit on top of settings changed elsewhere, and saving sends only them", async () => {
  const requests: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const body = await req.json();
      requests.push({ method: req.method, path: new URL(req.url).pathname, body });
      return new Response(JSON.stringify(transfer(body as object)), { status: 200 });
    }),
  );
  const qc = new QueryClient();
  const viewer = { userId: 2, username: "viewer", isAdmin: false } as Principal;
  qc.setQueryData(keys.me, viewer);
  qc.setQueryData<SettingsResponse>(keys.settings, {
    settings,
    locked: {},
    proxyPasswordSet: false,
  });
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={qc}>
      <SettingsForm groups={[speedGroup, altSpeedGroup]} />
    </QueryClientProvider>,
  );

  // Speed limits are open to users who aren't admins.
  await user.type(screen.getByLabelText("Download limit"), "500");
  expect(screen.getByText("1 unsaved change")).toBeTruthy();

  // Someone switches the alternative limits on in the transfer bar.
  act(() => {
    qc.setQueryData<SettingsResponse>(keys.settings, {
      settings: { ...settings, altSpeedEnabled: true },
      locked: {},
      proxyPasswordSet: false,
    });
  });
  const altSwitch = screen.getByRole("switch", { name: "Use the alternative limits" });
  await vi.waitFor(() => expect(altSwitch.getAttribute("aria-checked")).toBe("true"));
  expect((screen.getByLabelText("Download limit") as HTMLInputElement).value).toBe("500");
  expect(screen.getByText("1 unsaved change")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Save" }));
  await vi.waitFor(() => expect(screen.getByText("All changes saved")).toBeTruthy());
  expect(requests).toEqual([
    { method: "PATCH", path: "/api/piecemeal/v1/transfer", body: { downloadLimit: 512_000 } },
  ]);
  expect(altSwitch.getAttribute("aria-checked")).toBe("true");
});

test("the proxy password is write-only, and proxy fields show once a proxy is chosen", async () => {
  const requests: { method: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const body = await req.json();
      requests.push({ method: req.method, body });
      return new Response(
        JSON.stringify({
          settings: { ...configured, ...(body as object), proxyPassword: "" },
          locked: {},
          proxyPasswordSet: true,
        }),
        { status: 200 },
      );
    }),
  );
  const proxySettings = {
    proxyType: "none",
    proxyHost: "",
    proxyPort: 1080,
    proxyUsername: "",
    proxyPassword: "",
    proxyBitTorrent: true,
    proxyPeerConnections: true,
    proxyHostnameLookup: true,
    proxyRSS: true,
    proxyTorrentFiles: true,
  } as Settings;
  const configured = {
    ...proxySettings,
    proxyType: "socks5",
    proxyHost: "10.64.0.1",
    proxyUsername: "user",
  } as Settings;
  const qc = new QueryClient();
  qc.setQueryData(keys.me, { userId: 1, username: "admin", isAdmin: true } as Principal);
  qc.setQueryData<SettingsResponse>(keys.settings, {
    settings: proxySettings,
    locked: {},
    proxyPasswordSet: false,
  });
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={qc}>
      <SettingsForm groups={[proxyGroup]} />
    </QueryClientProvider>,
  );

  expect(screen.queryByLabelText("Host")).toBeNull();
  act(() => {
    qc.setQueryData<SettingsResponse>(keys.settings, {
      settings: configured,
      locked: {},
      proxyPasswordSet: true,
    });
  });
  const password = (await screen.findByLabelText("Password")) as HTMLInputElement;
  expect(password.type).toBe("password");
  expect(password.value).toBe("");
  expect(password.placeholder).toMatch(/Saved/);

  await user.type(password, "new-secret");
  await user.click(screen.getByRole("button", { name: "Save" }));
  await vi.waitFor(() => expect(screen.getByText("All changes saved")).toBeTruthy());
  expect(requests).toEqual([{ method: "PATCH", body: { proxyPassword: "new-secret" } }]);
  expect((screen.getByLabelText("Password") as HTMLInputElement).value).toBe("");
});
