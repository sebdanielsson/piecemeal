import { afterEach, expect, test, vi } from "vitest";
import { api } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("unsafe requests carry the CSRF header", async () => {
  const fetchMock = vi.fn<(req: Request) => Promise<Response>>(
    async () => new Response(null, { status: 204 }),
  );
  vi.stubGlobal("fetch", fetchMock);
  await api.PUT("/tags/{name}", { params: { path: { name: "x" } } });
  const req = fetchMock.mock.calls[0]?.[0];
  expect(req?.method).toBe("PUT");
  expect(req?.url).toMatch(/\/api\/piecemeal\/v1\/tags\/x$/);
  expect(req?.headers.get("X-Requested-With")).toBe("piecemeal");
});
