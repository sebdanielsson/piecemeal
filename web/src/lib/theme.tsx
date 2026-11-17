import { createContext, use, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { useMediaQuery } from "@/hooks/use-mobile";
import { readStorage, writeStorage } from "./storage";

export type ThemePreference = "system" | "light" | "dark";

// index.html reads the same key before the first paint, so there is no flash.
export const themeStorageKey = "piecemeal.theme";

interface ThemeContextValue {
  preference: ThemePreference;
  resolved: "light" | "dark";
  setPreference: (p: ThemePreference) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(() => {
    const stored = readStorage<string>(themeStorageKey, "system");
    return stored === "light" || stored === "dark" ? stored : "system";
  });
  const systemDark = useMediaQuery("(prefers-color-scheme: dark)");
  const resolved = preference === "system" ? (systemDark ? "dark" : "light") : preference;

  // A layout effect, so the class is in place before other effects read colours.
  useLayoutEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("dark", resolved === "dark");
    root.style.colorScheme = resolved;
  }, [resolved]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      preference,
      resolved,
      setPreference: (p) => {
        setPreferenceState(p);
        writeStorage(themeStorageKey, p);
      },
    }),
    [preference, resolved],
  );
  return <ThemeContext value={value}>{children}</ThemeContext>;
}

export function useTheme(): ThemeContextValue {
  const ctx = use(ThemeContext);
  if (!ctx) {
    throw new Error("useTheme must be used inside ThemeProvider");
  }
  return ctx;
}
