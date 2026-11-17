import { toast } from "sonner";

/**
 * Copies text. navigator.clipboard needs a secure context, which a
 * self-hosted instance on plain HTTP isn't, so fall back to execCommand.
 */
export async function copyText(text: string, what = "Copied"): Promise<boolean> {
  try {
    if (globalThis.isSecureContext && navigator.clipboard) {
      await navigator.clipboard.writeText(text);
      toast.success(what);
      return true;
    }
  } catch {
    // Fall through to the legacy path.
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.append(area);
  area.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  area.remove();
  if (ok) {
    toast.success(what);
  } else {
    toast.error("Couldn't copy to the clipboard");
  }
  return ok;
}
