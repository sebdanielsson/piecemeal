// Human-readable numbers for sizes, rates, times and limits. The API reports
// sizes in bytes, rates in bytes per second and durations in seconds.

const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"] as const;

const fractionDigits = (v: number) => (v < 10 ? 2 : v < 100 ? 1 : 0);

/** Formats a byte count with binary units and about three significant digits. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return "–";
  }
  if (bytes < 1024) {
    return `${Math.round(bytes)} B`;
  }
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  let text = value.toFixed(fractionDigits(value));
  // Rounding can carry over to the next unit: 1023.9 KiB shows as 1.00 MiB.
  if (Number(text) >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
    text = value.toFixed(fractionDigits(value));
  }
  return `${text} ${units[unit]}`;
}

/** Formats a rate in bytes per second. */
export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`;
}

/** Formats a rate, or returns an empty string when nothing is moving. */
export function formatActiveRate(bytesPerSecond: number): string {
  return bytesPerSecond > 0 ? formatRate(bytesPerSecond) : "";
}

/** Formats a speed limit in bytes per second, where 0 is unlimited. */
export function formatLimit(bytesPerSecond: number): string {
  return bytesPerSecond > 0 ? formatRate(bytesPerSecond) : "Unlimited";
}

const day = 86_400;
const hour = 3_600;
const minute = 60;

/**
 * Formats a duration in seconds with its two largest units, such as
 * "2d 4h", "3h 12m", "5m 9s" or "42s".
 */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "∞";
  }
  const s = Math.floor(seconds);
  if (s < minute) {
    return `${s}s`;
  }
  if (s < hour) {
    return `${Math.floor(s / minute)}m ${s % minute}s`;
  }
  if (s < day) {
    return `${Math.floor(s / hour)}h ${Math.floor((s % hour) / minute)}m`;
  }
  if (s < 365 * day) {
    return `${Math.floor(s / day)}d ${Math.floor((s % day) / hour)}h`;
  }
  return `${Math.floor(s / (365 * day))}y ${Math.floor((s % (365 * day)) / day)}d`;
}

/** Formats an ETA in seconds; the API reports -1 when it is unknown. */
export function formatEta(seconds: number): string {
  if (seconds < 0 || seconds >= 100 * day) {
    return "∞";
  }
  if (seconds === 0) {
    return "";
  }
  return formatDuration(seconds);
}

/** Formats a share ratio. */
export function formatRatio(ratio: number): string {
  if (!Number.isFinite(ratio) || ratio < 0) {
    return "∞";
  }
  return ratio.toFixed(2);
}

/**
 * Formats a 0 to 1 fraction as a percentage. It never rounds up to 100%, so
 * "100%" always means done.
 */
export function formatPercent(fraction: number): string {
  if (!Number.isFinite(fraction) || fraction <= 0) {
    return "0%";
  }
  if (fraction >= 1) {
    return "100%";
  }
  const pct = Math.floor(fraction * 1000) / 10;
  return `${pct.toFixed(1)}%`;
}

const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** Formats a Unix time in seconds; 0 means never. */
export function formatUnixTime(unix: number): string {
  return unix > 0 ? dateTime.format(new Date(unix * 1000)) : "–";
}

/** Formats an ISO 8601 date-time string from the API. */
export function formatDateTime(iso: string | undefined): string {
  if (!iso) {
    return "–";
  }
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "–" : dateTime.format(d);
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** Formats a time relative to now, such as "5 minutes ago". */
export function formatRelative(date: Date | string | undefined, now = Date.now()): string {
  if (!date) {
    return "never";
  }
  const d = typeof date === "string" ? new Date(date) : date;
  const diff = (d.getTime() - now) / 1000;
  if (Number.isNaN(diff)) {
    return "–";
  }
  const abs = Math.abs(diff);
  if (abs < 45) {
    return "just now";
  }
  if (abs < hour) {
    return relative.format(Math.round(diff / minute), "minute");
  }
  if (abs < day) {
    return relative.format(Math.round(diff / hour), "hour");
  }
  if (abs < 30 * day) {
    return relative.format(Math.round(diff / day), "day");
  }
  if (abs < 365 * day) {
    return relative.format(Math.round(diff / (30 * day)), "month");
  }
  return relative.format(Math.round(diff / (365 * day)), "year");
}

/**
 * The host of a tracker URL, used to group torrents by tracker. Returns ""
 * for no tracker and the input itself when it isn't a URL.
 */
export function trackerHost(url: string): string {
  if (!url) {
    return "";
  }
  try {
    return new URL(url).hostname || url;
  } catch {
    return url;
  }
}

/** Describes a per-torrent share limit: -2 follows the global setting, -1 is unlimited. */
export function formatShareLimit(value: number, kind: "ratio" | "minutes"): string {
  if (value === -2) {
    return "Global";
  }
  if (value < 0) {
    return "Unlimited";
  }
  return kind === "ratio" ? value.toFixed(2) : formatDuration(value * 60);
}

/** A count with its noun, pluralised in English: "1 torrent", "3 torrents". */
export function plural(count: number, noun: string, pluralNoun = `${noun}s`): string {
  return `${count.toLocaleString()} ${count === 1 ? noun : pluralNoun}`;
}
