import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./schema";

export const apiPrefix = "/api/piecemeal/v1";

// Session-cookie requests that change state must carry this header; see
// internal/api's CSRF check.
const csrf: Middleware = {
  onRequest({ request }) {
    if (request.method !== "GET" && request.method !== "HEAD") {
      request.headers.set("X-Requested-With", "piecemeal");
    }
    return request;
  },
};

// Absolute, because fetch outside a browser (tests) has no base URL.
export const api = createClient<paths>({
  baseUrl: `${globalThis.location.origin}${apiPrefix}`,
  credentials: "same-origin",
  // Looked up per request, so tests can stub it.
  fetch: (request) => globalThis.fetch(request),
});
api.use(csrf);
