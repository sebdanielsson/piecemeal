import { useCallback, useState } from "react";
import { readStorage, writeStorage } from "@/lib/storage";

type SetState<T> = (next: T | ((prev: T) => T)) => void;

/** useState that remembers its value in localStorage. */
export function usePersistentState<T>(
  key: string,
  initial: T,
  validate?: (v: unknown) => v is T,
): [T, SetState<T>] {
  const [value, setValue] = useState<T>(() => {
    const stored = readStorage<unknown>(key, initial);
    return validate && !validate(stored) ? initial : (stored as T);
  });
  const set = useCallback<SetState<T>>(
    (next) => {
      setValue((prev) => {
        const v = typeof next === "function" ? (next as (p: T) => T)(prev) : next;
        writeStorage(key, v);
        return v;
      });
    },
    [key],
  );
  return [value, set];
}
