/**
 * Copy `value` to the clipboard, resolving to whether it succeeded.
 *
 * The async Clipboard API (`navigator.clipboard`) only exists in a secure context — HTTPS or
 * localhost. Swallow's dashboard is frequently served over plain HTTP on a LAN, where
 * `window.isSecureContext` is false and `navigator.clipboard` is undefined, so a direct
 * `writeText` throws and copy silently does nothing. This helper falls back to a detached
 * `<textarea>` plus the legacy `document.execCommand('copy')`, which works on insecure origins,
 * and returns `false` only when both paths fail so callers can show a failure state.
 *
 * It is the single source for clipboard writes in the dashboard; compose it rather than calling
 * `navigator.clipboard` directly.
 */
export async function copyText(value: string): Promise<boolean> {
  if (typeof navigator !== "undefined" && navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      // Permission denied or transient failure: fall through to the execCommand fallback.
    }
  }
  return copyViaTextarea(value);
}

/**
 * Insecure-origin fallback: place the text in an off-screen, read-only textarea, select it, and
 * run the legacy copy command. Any prior user selection is restored so copying never disturbs
 * what the operator had highlighted.
 */
function copyViaTextarea(value: string): boolean {
  if (typeof document === "undefined") return false;
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.top = "-9999px";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);

  const selection = document.getSelection();
  const previousRange =
    selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null;

  textarea.select();
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } catch {
    copied = false;
  }

  document.body.removeChild(textarea);
  if (previousRange && selection) {
    selection.removeAllRanges();
    selection.addRange(previousRange);
  }
  return copied;
}
