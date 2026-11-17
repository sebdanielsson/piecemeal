// localStorage can be missing or throw (private windows, blocked site data),
// so every access is guarded and falls back to the default.

export function readStorage<T>(key: string, fallback: T): T {
  try {
    const raw = globalThis.localStorage?.getItem(key);
    if (raw == null) {
      return fallback;
    }
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

export function writeStorage(key: string, value: unknown): void {
  try {
    globalThis.localStorage?.setItem(key, JSON.stringify(value));
  } catch {
    // Not persisting a preference is fine.
  }
}
