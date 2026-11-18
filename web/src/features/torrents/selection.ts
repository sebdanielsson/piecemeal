// List selection as pure functions: click, Ctrl/Cmd-click, Shift-click and
// keyboard movement, over the list's current order of infohashes.

export interface Selection {
  selected: ReadonlySet<string>;
  /** Where a Shift range starts. */
  anchor: string | null;
  /** The row the keyboard is on. */
  cursor: string | null;
}

export const emptySelection: Selection = { selected: new Set(), anchor: null, cursor: null };

export interface ClickModifiers {
  /** Shift: select the range from the anchor. */
  range?: boolean;
  /** Ctrl or Cmd, or a checkbox: toggle one row. */
  toggle?: boolean;
}

function range(order: readonly string[], from: string | null, to: string): string[] {
  const end = order.indexOf(to);
  const start = from === null ? -1 : order.indexOf(from);
  if (end < 0) {
    return [];
  }
  if (start < 0) {
    return [to];
  }
  const [lo, hi] = start <= end ? [start, end] : [end, start];
  return order.slice(lo, hi + 1);
}

export function clickSelect(
  sel: Selection,
  hash: string,
  order: readonly string[],
  mods: ClickModifiers = {},
): Selection {
  if (mods.range) {
    const anchor = sel.anchor ?? hash;
    const rows = range(order, anchor, hash);
    const selected = new Set(mods.toggle ? sel.selected : []);
    for (const h of rows) {
      selected.add(h);
    }
    return { selected, anchor, cursor: hash };
  }
  if (mods.toggle) {
    const selected = new Set(sel.selected);
    if (selected.has(hash)) {
      selected.delete(hash);
    } else {
      selected.add(hash);
    }
    return { selected, anchor: hash, cursor: hash };
  }
  return { selected: new Set([hash]), anchor: hash, cursor: hash };
}

/** Moves the cursor by delta rows; with extend, grows the range from the anchor. */
export function moveCursor(
  sel: Selection,
  order: readonly string[],
  delta: number,
  extend = false,
): Selection {
  if (order.length === 0) {
    return sel;
  }
  const at = sel.cursor === null ? -1 : order.indexOf(sel.cursor);
  const next =
    at < 0
      ? delta > 0
        ? 0
        : order.length - 1
      : Math.max(0, Math.min(order.length - 1, at + delta));
  const hash = order[next] as string;
  if (extend) {
    const anchor = sel.anchor ?? sel.cursor ?? hash;
    return { selected: new Set(range(order, anchor, hash)), anchor, cursor: hash };
  }
  return { selected: new Set([hash]), anchor: hash, cursor: hash };
}

export function selectAll(sel: Selection, order: readonly string[]): Selection {
  return { ...sel, selected: new Set(order) };
}

/** The selected hashes that are in the list, in list order. */
export function visibleSelection(sel: Selection, order: readonly string[]): string[] {
  if (sel.selected.size === 0) {
    return [];
  }
  return order.filter((h) => sel.selected.has(h));
}
