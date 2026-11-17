// jsdom lacks layout and a few browser APIs the app uses; these stand-ins
// are just enough for components to render in tests.
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

afterEach(() => {
  cleanup();
  localStorage.clear();
});

class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= NoopResizeObserver as unknown as typeof ResizeObserver;

globalThis.matchMedia ??= ((query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addEventListener: () => {},
  removeEventListener: () => {},
  addListener: () => {},
  removeListener: () => {},
  dispatchEvent: () => false,
})) as typeof globalThis.matchMedia;

// Virtualized lists only render rows that fit their scroll container.
for (const [prop, value] of [
  ["offsetHeight", 600],
  ["offsetWidth", 1000],
  ["clientHeight", 600],
  ["clientWidth", 1000],
] as const) {
  Object.defineProperty(HTMLElement.prototype, prop, { configurable: true, get: () => value });
}
Element.prototype.scrollTo ??= vi.fn<() => void>() as unknown as typeof Element.prototype.scrollTo;
Element.prototype.scrollIntoView ??= vi.fn<() => void>();
