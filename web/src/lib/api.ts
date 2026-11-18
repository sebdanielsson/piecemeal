import type { Middleware } from "openapi-fetch";
import { api } from "@/api/client";
import type { ProblemDetails } from "@/api/types";

/** An API error with the RFC 9457 problem details the server sent. */
export class ApiError extends Error {
  readonly status: number;
  readonly problem: ProblemDetails | undefined;

  constructor(status: number, problem: ProblemDetails | undefined) {
    super(problemMessage(status, problem));
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }
}

/** A readable message from problem details, including field errors. */
export function problemMessage(status: number, problem: ProblemDetails | undefined): string {
  if (!problem || typeof problem !== "object") {
    return `Request failed (${status})`;
  }
  const base = problem.detail || problem.title || `Request failed (${status})`;
  const details = (problem.errors ?? [])
    .map((e) => [e.location?.replace(/^body\./, ""), e.message].filter(Boolean).join(": "))
    .filter(Boolean);
  return details.length > 0 ? `${base}: ${details.join("; ")}` : base;
}

/** The message to show for any error a mutation or query throws. */
export function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

type FetchResult<T> =
  | { data: T; error?: never; response: Response }
  | { data?: never; error: unknown; response: Response };

/** Resolves to the response body, or throws an ApiError. */
export async function unwrap<T>(request: Promise<FetchResult<T>>): Promise<Exclude<T, undefined>> {
  const { data, error, response } = await request;
  if (!response.ok) {
    throw new ApiError(response.status, error as ProblemDetails | undefined);
  }
  return data as Exclude<T, undefined>;
}

let onUnauthorized: (() => void) | undefined;

/**
 * Registers what happens when the session ends under us: any API call
 * answering 401, except the sign-in endpoints themselves.
 */
export function setUnauthorizedHandler(handler: (() => void) | undefined): void {
  onUnauthorized = handler;
}

const unauthorized: Middleware = {
  onResponse({ request, response }) {
    if (response.status === 401 && !new URL(request.url).pathname.includes("/auth/")) {
      onUnauthorized?.();
    }
    return response;
  },
};
api.use(unauthorized);
