import toast from "react-hot-toast";

/** Shown when the browser refuses both copy methods (typically plain HTTP). */
export const COPY_BLOCKED_HINT =
  "复制失败：当前是 HTTP 访问，浏览器只允许在 HTTPS 页面中复制。请使用 HTTPS 访问面板，或长按文字手动复制";

function legacyCopy(text: string): boolean {
  if (typeof document === "undefined") return false;
  const ta = document.createElement("textarea");

  ta.value = text;
  ta.setAttribute("readonly", "");
  // 16px avoids iOS zooming in; off-screen keeps it invisible.
  ta.style.cssText =
    "position:fixed;top:0;left:-9999px;opacity:0;font-size:16px;";
  // An open dialog traps focus, so the textarea must live inside it.
  const host =
    document.activeElement?.closest('[role="dialog"]') ?? document.body;

  host.appendChild(ta);
  ta.focus();
  ta.select();
  ta.setSelectionRange(0, text.length);
  let ok = false;

  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  ta.remove();

  return ok;
}

/**
 * Copies text: the Clipboard API where the page is a secure context, else
 * the selection-based fallback, which most browsers still allow over HTTP.
 */
export async function writeClipboard(text: string): Promise<boolean> {
  if (
    typeof navigator !== "undefined" &&
    navigator.clipboard &&
    typeof window !== "undefined" &&
    window.isSecureContext
  ) {
    try {
      await navigator.clipboard.writeText(text);

      return true;
    } catch {
      // Fall through to the legacy method.
    }
  }

  return legacyCopy(text);
}

export function copyFailedHint(): string {
  return typeof window !== "undefined" && !window.isSecureContext
    ? COPY_BLOCKED_HINT
    : "复制失败，请手动选择文字复制";
}

/** Copies and reports the outcome with a toast. */
export async function copyWithToast(
  text: string,
  success = "已复制",
): Promise<boolean> {
  const ok = await writeClipboard(text);

  if (ok) {
    toast.success(success);
  } else {
    toast.error(copyFailedHint(), { duration: 6000 });
  }

  return ok;
}
