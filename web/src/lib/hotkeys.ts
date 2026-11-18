import { useEffect, useEffectEvent } from "react";

/** Whether a key press is meant for a text field rather than a shortcut. */
export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  if (target.isContentEditable) {
    return true;
  }
  const tag = target.tagName;
  if (tag === "TEXTAREA" || tag === "SELECT") {
    return true;
  }
  if (tag === "INPUT") {
    const type = (target as HTMLInputElement).type;
    return !["checkbox", "radio", "button", "submit", "reset", "range", "color"].includes(type);
  }
  return false;
}

// Keys that controls use themselves: Enter and Space press a button, arrows
// move through tabs and menus.
const controlKeys = new Set(["Enter", "Space", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"]);

/** Whether the key press belongs to a focused control rather than a shortcut. */
export function isControlTarget(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    target.closest(
      'button, a[href], summary, [role="button"], [role="checkbox"], [role="switch"], [role="tab"], [role="menuitem"], [role="option"], [role="slider"], [role="separator"]',
    ) !== null
  );
}

/** Whether a dialog, menu or sheet is open, which owns the keyboard. */
export function isOverlayOpen(): boolean {
  return (
    document.querySelector(
      '[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]',
    ) !== null
  );
}

/**
 * Normalises a key event to a binding name: "a", "?", "/", "Delete",
 * "Space", "Escape", "ArrowDown", "mod+a" and so on. "mod" is Cmd on macOS
 * and Ctrl elsewhere.
 */
export function bindingName(e: KeyboardEvent): string {
  let key = e.key === " " ? "Space" : e.key.length === 1 ? e.key.toLowerCase() : e.key;
  if (e.key === "?" || e.key === "/") {
    key = e.key;
  }
  const mod = e.metaKey || e.ctrlKey;
  return mod ? `mod+${key}` : e.altKey ? `alt+${key}` : key;
}

export type Bindings = Record<string, (e: KeyboardEvent) => void>;

/**
 * Global keyboard shortcuts. They don't fire while typing in a field or
 * while a dialog or menu is open, so those keep their own keys.
 */
export function useHotkeys(bindings: Bindings, enabled = true): void {
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.defaultPrevented || e.isComposing || isTypingTarget(e.target) || isOverlayOpen()) {
      return;
    }
    const name = bindingName(e);
    if (controlKeys.has(name) && isControlTarget(e.target)) {
      return;
    }
    const handler = bindings[name];
    if (handler) {
      e.preventDefault();
      handler(e);
    }
  });
  useEffect(() => {
    if (!enabled) {
      return;
    }
    const listener = (e: KeyboardEvent) => onKey(e);
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, [enabled]);
}

export const shortcutHelp: { keys: string[]; description: string }[] = [
  { keys: ["/"], description: "Search torrents" },
  { keys: ["a"], description: "Add torrents" },
  { keys: ["↑", "↓"], description: "Move through the list" },
  { keys: ["Shift", "↑ ↓"], description: "Extend the selection" },
  { keys: ["Enter"], description: "Open the torrent's details" },
  { keys: ["Space"], description: "Start or stop the selected torrents" },
  { keys: ["Delete"], description: "Remove the selected torrents" },
  { keys: ["⌘/Ctrl", "A"], description: "Select every torrent in the list" },
  { keys: ["Esc"], description: "Clear the selection or close the details" },
  { keys: ["⌘/Ctrl", "V"], description: "Paste magnet links to add them" },
  { keys: ["⌘/Ctrl", "B"], description: "Show or hide the sidebar" },
  { keys: ["?"], description: "Show these shortcuts" },
];
