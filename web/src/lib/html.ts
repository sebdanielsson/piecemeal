/**
 * The text of an HTML fragment from a feed, without its markup. Parsing
 * with DOMParser runs no scripts and loads nothing, and the result is used
 * as text, never inserted as HTML.
 */
export function htmlToText(html: string): string {
  if (!html.includes("<") && !html.includes("&")) {
    return html.trim();
  }
  const doc = new DOMParser().parseFromString(html, "text/html");
  for (const el of doc.querySelectorAll("script, style")) {
    el.remove();
  }
  return (doc.body.textContent ?? "").replace(/\s+/g, " ").trim();
}
